package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
)

// renderTemplate - used in most handlers to render the html output
func renderTemplate(w http.ResponseWriter, data interface{}, path ...string) {

	t, err := template.New("layout.html").Funcs(funcMap).ParseFiles(path...)

	if err != nil {
		fmt.Fprint(w, "Error:", err)
		fmt.Println("Error:", err)
		return
	}

	err = t.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		fmt.Fprint(w, "Error:", err)
		fmt.Println("Error:", err)
	}

}

// task comment show only date not time
func myTruncateDate(t time.Time) string {

	// convert time to string
	s := t.String()

	// lop off time (00:44:20 +0000 UTC) and only leave date (2019-04-11)
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func uploadFile(client *storage.Client, bucket, job string, object string) error {
	ctx := context.Background()
	// [START upload_file]
	f, err := os.Open("./staging/" + object)
	if err != nil {
		return err
	}
	defer f.Close()

	wc := client.Bucket(bucket).Object(fmt.Sprintf("%s/%s", job, object)).NewWriter(ctx)
	if _, err = io.Copy(wc, f); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	// [END upload_file]
	return nil
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
		panic(fmt.Sprintf("%s: %s", msg, err))
	}
}

// https://groups.google.com/forum/#!topic/golang-dev/N2esIxEafGc
func unescapeJS(v interface{}) template.JS {
	return template.JS(fmt.Sprint(v))
}
