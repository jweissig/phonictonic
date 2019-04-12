// FYI - I know about the hardcoded username, password, and ips here.
// this is part of the tutorial.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"github.com/streadway/amqp"
	"github.com/twinj/uuid"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
)

// route = / (GET) - show upload page
func showUploadHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	bootstrap := ctx.Value(contextBootstrap).(Bootstrap)

	data := struct {
		Errors    map[string]string
		Title     string
		Bootstrap Bootstrap
		Job       string
		Tasks     []Task
	}{
		Errors:    make(map[string]string),
		Title:     "Phonic Tonic",
		Bootstrap: bootstrap,
		Job:       fmt.Sprintf("%s", uuid.NewV4()),
		Tasks:     nil,
	}

	renderTemplate(w, data, "templates/layout.html", "templates/upload.html")

}

// route = / (POST) - save upload page & create job
func processUploadHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	bootstrap := ctx.Value(contextBootstrap).(Bootstrap)

	// error handeling
	userEmail := r.FormValue("email")
	userJob := r.FormValue("job")

	// collect all errors
	formErrors := make(map[string]string)

	// process email
	if !strings.ContainsRune(userEmail, '@') {
		formErrors["Email"] = "Please enter your email address"
	}

	// check to make sure task were uploaded
	tasksCount := getTasksCount(userJob)
	if tasksCount == 0 {
		formErrors["Files"] = "Please upload some files"
	}

	// get uploaded files (if they exist)
	// https://stackoverflow.com/questions/17759286/how-can-i-show-you-the-files-already-stored-on-server-using-dropzone-js
	tasks, err := getTasks(userJob)
	if err != nil {
		log.Println(err)
	}

	//
	// ERROR - if we find error return to page w/ errors
	//
	if len(formErrors) != 0 {

		data := struct {
			Errors    map[string]string
			Title     string
			Bootstrap Bootstrap
			Job       string
			Tasks     []Task
		}{
			Errors:    formErrors,
			Title:     "Phonic Tonic",
			Bootstrap: bootstrap,
			Job:       userJob,
			Tasks:     tasks,
		}

		renderTemplate(w, data, "templates/layout.html", "templates/upload.html")
		return

	}

	// add job to database
	job := Job{
		JobUUID: userJob,
		Email:   userEmail,
	}
	db.Create(&job)

	// queue all tasks for transcoding

	// make sure queue is defined
	q, err := ch.QueueDeclare(
		"task_transcode", // name
		true,             // durable
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	//failOnError(err, "Failed to declare a queue")
	if err != nil {
		log.Print("Failed to declare a queue")
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	// loop over tasks and add them to the queue
	for _, task := range tasks {

		// add message to queue
		err = ch.Publish(
			"",     // exchange
			q.Name, // routing key
			false,  // mandatory
			false,
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "text/plain",
				Body:         []byte(fmt.Sprintf("%s/%s", task.JobUUID, task.TaskUUID)),
			})
		//failOnError(err, "Failed to publish a message")
		if err != nil {
			log.Print("Failed to publish a message")
			http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
			return
		}
		log.Printf(" [x] Sent %s", fmt.Sprintf("%s/%s", task.JobUUID, task.TaskUUID))

	}

	// redirect page
	http.Redirect(w, r, fmt.Sprintf("/result/%s", userJob), 301)
	return

}

// route = /upload (POST) - save & probe file
func saveHandler(w http.ResponseWriter, r *http.Request) {

	// gen uuids
	jobID := r.FormValue("job")
	taskID := fmt.Sprintf("%s", uuid.NewV4())

	r.ParseMultipartForm(32 << 20) // 32MB is the default used by FormFile
	file, handler, err := r.FormFile("file")
	//file, _, err := r.FormFile("file")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()

	// save the file to disk
	f, err := os.OpenFile("./staging/"+fmt.Sprintf("%s", taskID), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println(err)
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	io.Copy(f, file)

	// get the file size
	stat, err := f.Stat()
	if err != nil {
		fmt.Println(err)
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	// ffprobe detect
	probedata, err := Probe("./staging/" + taskID)
	if probedata == nil || err != nil {

		log.Print(err)

		// if we cannot detect the type return a 415 error (we should log an error here too)
		http.Error(w, "415 Unsupported Media Type", http.StatusUnsupportedMediaType)

		// remove the file from the local filesystem
		err = os.Remove("./staging/" + taskID)
		if err != nil {
			fmt.Println(err)
		}

		// return the 415 error to the user
		return
	}

	// check for valid audio track
	if probedata.Format.DurationSeconds == 0 {
		// no audio track detected
		http.Error(w, "415 Unsupported Media Type", http.StatusUnsupportedMediaType)

		// remove the file from the local filesystem
		err = os.Remove("./staging/" + taskID)
		if err != nil {
			fmt.Println(err)
		}

		return
	}

	log.Printf("Format FormatName: %v", probedata.Format.FormatName)
	log.Printf("Format DurationSeconds: %v", probedata.Format.DurationSeconds)

	// create client to google storage
	gcsctx := context.Background()
	client, err := storage.NewClient(gcsctx)
	if err != nil {
		log.Print(err)
	}

	// upload raw
	if err = uploadFile(client, "phonic-test-staging", jobID, taskID); err != nil {
		log.Printf("Cannot write object: %v", err)
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Printf("Uploaded to Google Storage: %s", taskID)

	// remove the file from the local filesystem
	err = os.Remove("./staging/" + taskID)
	if err != nil {
		fmt.Println(err)
	}

	// add task to database
	task := Task{
		JobUUID:         jobID,
		TaskUUID:        fmt.Sprintf("%s", taskID),
		Status:          "Processing", // set default status
		Filename:        handler.Filename,
		FileSize:        stat.Size(),
		FormatName:      probedata.Format.FormatName,      //data.Format.FormatName,
		DurationSeconds: probedata.Format.DurationSeconds, //data.Format.DurationSeconds,
	}

	db.Create(&task)
	return

}

//
// results pickup page
//
func resultHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	bootstrap := ctx.Value(contextBootstrap).(Bootstrap)

	// uuid
	vars, _ := ctx.Value(contextRouteVars).(map[string]string)

	// grab values from url path
	jobID := vars["uuid"]

	job, err := getJob(jobID)
	if err != nil {
		log.Println(err)
	}

	tasks, err := getTasks(jobID)
	if err != nil {
		log.Println(err)
	}

	// used to reload results page when results still processing
	reload := 0

	// loop through tasks and see if they are all complete (used for reloading results page)
	for _, task := range tasks {
		if task.Status != "Completed" {
			reload = 1
		}
	}

	data := struct {
		Title     string
		Bootstrap Bootstrap
		Job       Job
		Tasks     []Task
		Reload    int
	}{
		Title:     "Your Results",
		Bootstrap: bootstrap,
		Job:       job,
		Tasks:     tasks,
		Reload:    reload,
	}

	renderTemplate(w, data, "templates/layout.html", "templates/results.html")

}

func resultTaskHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	bootstrap := ctx.Value(contextBootstrap).(Bootstrap)

	// uuid
	vars, _ := ctx.Value(contextRouteVars).(map[string]string)

	// grab values from url path
	jobID := vars["juuid"]
	taskID := vars["tuuid"]

	job, err := getJob(jobID)
	if err != nil {
		log.Println(err)
	}

	task, err := getTaskByID(jobID, taskID)
	if err != nil {
		log.Println(err)
	}

	// init struct for transcript
	var response Response
	var transcript string

	// parse json into struct
	json.Unmarshal([]byte(task.JSONRaw), &response)

	// loop through json and pull out the transcripts
	for _, result := range response.Results {
		for _, alternative := range result.Alternatives {
			transcript = transcript + " " + alternative.Transcript
		}
	}

	data := struct {
		Title      string
		Bootstrap  Bootstrap
		Job        Job
		Task       Task
		Transcript string
		PrettyJSON string
	}{
		Title:      "Result Details",
		Bootstrap:  bootstrap,
		Job:        job,
		Task:       task,
		Transcript: strings.TrimSpace(transcript),
		PrettyJSON: task.JSONRaw,
	}

	renderTemplate(w, data, "templates/layout.html", "templates/result_task.html")

}

//
// admin - show all jobs
//
func jobsHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	bootstrap := ctx.Value(contextBootstrap).(Bootstrap)

	// TODO: lock with down with user/login but good enough for now
	if bootstrap.ForwardedFor != adminIP {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "404")
		return
	}

	jobs, err := getAllJobs()
	if err != nil {
		log.Println(err)
	}

	data := struct {
		Title     string
		Bootstrap Bootstrap
		Jobs      []Job
	}{
		Title:     "Jobs",
		Bootstrap: bootstrap,
		Jobs:      jobs,
	}

	t, err := template.New("layout_admin.html").Funcs(funcMap).ParseFiles("templates/layout_admin.html", "templates/admin_jobs.html")
	if err != nil {
		fmt.Fprint(w, "Error:", err)
		fmt.Println("Error:", err)
		return
	}

	err = t.ExecuteTemplate(w, "layout_admin.html", data)
	if err != nil {
		fmt.Fprint(w, "Error:", err)
		fmt.Println("Error:", err)
	}

}

//
// admin - show specific job
//
func jobHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()
	bootstrap := ctx.Value(contextBootstrap).(Bootstrap)

	// TODO: lock with down with user/login but good enough for now
	if bootstrap.ForwardedFor != adminIP {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "404")
		return
	}

	// uuid
	vars, _ := ctx.Value(contextRouteVars).(map[string]string)

	// grab values from url path
	jobID := vars["uuid"]

	job, err := getJob(jobID)
	if err != nil {
		log.Println(err)
	}

	tasks, err := getTasks(jobID)
	if err != nil {
		log.Println(err)
	}

	data := struct {
		Title     string
		Bootstrap Bootstrap
		Job       Job
		Tasks     []Task
	}{
		Title:     "Jobs",
		Bootstrap: bootstrap,
		Job:       job,
		Tasks:     tasks,
	}

	t, err := template.New("layout_admin.html").Funcs(funcMap).ParseFiles("templates/layout_admin.html", "templates/admin_job_details.html")
	if err != nil {
		fmt.Fprint(w, "Error:", err)
		fmt.Println("Error:", err)
		return
	}

	err = t.ExecuteTemplate(w, "layout_admin.html", data)
	if err != nil {
		fmt.Fprint(w, "Error:", err)
		fmt.Println("Error:", err)
	}

}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, "404")
}

func bootstrapHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// for data passing between middleware
		ctx := r.Context()

		// set routes used in many handlers
		routeVars := mux.Vars(r)
		ctx = context.WithValue(ctx, contextRouteVars, routeVars)

		// get current url for redirect if needed
		path := r.URL.Path
		if path == "" { // should not happen, but avoid a crash if it does
			path = "/"
		}

		// force www.phonictonic.com -> phonictonic.com
		if r.Host == "www.phonictonic.com" {
			http.Redirect(w, r, "https://phonictonic.com"+path, 301)
			return
		}

		// force http -> https upgrade
		if r.Header.Get("X-Forwarded-Proto") == "http" {
			http.Redirect(w, r, "https://phonictonic.com"+path, 301)
			return
		}

		// used for passing user/request data into the template
		var bootstrap Bootstrap

		// set values for page footer
		bootstrap.Hostname = hostname
		bootstrap.BuildDate = builddate
		bootstrap.BuildRevision = buildrevision

		// populate user ip
		forwardedFor := strings.Split(r.Header.Get("X-Forwarded-For"), ", ")
		bootstrap.ForwardedFor = forwardedFor[0]

		// pass bootstrap context down the line
		ctx = context.WithValue(ctx, contextBootstrap, bootstrap)

		next(w, r.WithContext(ctx))

	}
}

var (
	contextRouteVars = contextKey("routeVars")
	contextBootstrap = contextKey("bootstrap")
	adminIP          = "100.000.000.000"
	db               *gorm.DB
	conn             *amqp.Connection
	ch               *amqp.Channel
	router           = mux.NewRouter()
	builddate        = "2019-04-14"
	buildrevision    = ""
	hostname         = ""
	funcMap          = template.FuncMap{
		"unescapeJS":     unescapeJS,
		"myTruncateDate": myTruncateDate,
	}
)

// Bootstrap -
type Bootstrap struct {
	Hostname      string
	BuildDate     string
	BuildRevision string
	ForwardedFor  string
}

// https://medium.com/@matryer/context-keys-in-go-5312346a868d
type contextKey string

func (c contextKey) String() string {
	return "mypackage context key " + string(c)
}

func main() {

	// set hostname (used for debugging)
	hostname, _ = os.Hostname()

	// sql db connection
	var dbErr error
	db, dbErr = gorm.Open("mysql", "phonic-prod:qCCgQKIJg9FWU0ox@tcp(127.0.0.1:3306)/phonic_prod?charset=utf8&parseTime=True&loc=UTC")

	// check for error
	if dbErr != nil {
		//panic("Failed to connect database (is the container started?).")
		log.Println(dbErr)
	} else {
		log.Println("connected to mysql!")
	}

	// config db logger
	db.LogMode(true)

	// connect to rabbitmq
	var mqErr error
	conn, mqErr = amqp.Dial("amqp://B006DCF4:788A2F817EF9@35.000.000.000:5672/")
	if mqErr != nil {
		log.Print("Failed to connect to RabbitMQ")
	}
	defer conn.Close()

	var chErr error
	ch, chErr = conn.Channel()
	if chErr != nil {
		log.Print("Failed to open a channel")
	}
	defer ch.Close()

	// static assets (css, js, etc)
	http.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

	// routes
	router.HandleFunc("/", bootstrapHandler(processUploadHandler)).Methods("POST")    // submit job
	router.HandleFunc("/", bootstrapHandler(showUploadHandler))                       // html from w/ dropzone
	router.HandleFunc("/upload", bootstrapHandler(saveHandler))                       // save task (file upload)
	router.HandleFunc("/result/{uuid}", bootstrapHandler(resultHandler))              // results
	router.HandleFunc("/result/{juuid}/{tuuid}", bootstrapHandler(resultTaskHandler)) // result task

	// admin
	router.HandleFunc("/admin", bootstrapHandler(jobsHandler))
	router.HandleFunc("/admin/{uuid}", bootstrapHandler(jobHandler))

	// 404
	router.NotFoundHandler = http.HandlerFunc(notFound)

	http.Handle("/", router)

	log.Println("Listening 8081...")
	http.ListenAndServe(":8081", handlers.LoggingHandler(os.Stdout, http.DefaultServeMux))

}
