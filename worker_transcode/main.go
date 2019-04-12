// FYI - I know about the hardcoded username, password, and ips here.
// this is part of the tutorial.
package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/streadway/amqp"
	"golang.org/x/net/context"
)

func main() {

	fmt.Println("booting transcode worker...")

	// create client to google storage
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// connect to rabbitmq
	conn, err := amqp.Dial("amqp://B006DCF4:788A2F817EF9@35.000.000.000:5672/")
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	// define task_transcode queue if it doesn't already exist
	q, err := ch.QueueDeclare(
		"task_transcode", // name
		true,             // durable
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	failOnError(err, "Failed to declare a queue")

	// define task_transcribe queue if it doesn't already exist
	t, err := ch.QueueDeclare(
		"task_transcribe", // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	failOnError(err, "Failed to declare a queue")

	err = ch.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	failOnError(err, "Failed to set QoS")

	// get a message off the queue
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	forever := make(chan bool)

	// run through the messages
	go func() {
		for d := range msgs {

			// parse message
			message := strings.Split(fmt.Sprintf("%s", d.Body), "/")
			jobID, taskID := message[0], message[1]
			log.Printf("Received a message: %s/%s", jobID, taskID)

			// get source media file from GCS
			data, err := readFile(client, "phonic-test-staging", fmt.Sprintf("%s/%s", jobID, taskID))
			if err != nil {
				log.Fatalf("Cannot read object: %v", err)
			}
			log.Printf("Downloaded from Google Storage: %s", d.Body)

			// save source media file to disk
			saveErr := ioutil.WriteFile("./staging/"+taskID, data, 0666)
			if saveErr != nil {
				log.Fatalf("Cannot read object: %v", err)
			}
			log.Printf("Saved to disk: %s", taskID)

			// transcode source media
			log.Printf("Transcoding: %s", taskID)
			transcodeOutput, err := transcode("./staging/" + taskID)
			if err != nil {
				log.Print(err)
			}
			fmt.Println(transcodeOutput)

			// upload transcode media to GCS
			if err = uploadFile(client, "phonic-test-staging", jobID, fmt.Sprintf("%s.wav", taskID)); err != nil {
				log.Fatalf("Cannot write object: %v", err)
			}
			log.Printf("Uploaded to Google Storage: %s.wav", d.Body)

			// clean up and remove the files from the local filesystem
			err = os.Remove("./staging/" + taskID)
			if err != nil {
				fmt.Println(err)
			}
			// remove newly transcoded file too
			err = os.Remove("./staging/" + taskID + ".wav")
			if err != nil {
				fmt.Println(err)
			}

			// add transcribe message into the queue
			err = ch.Publish(
				"",     // exchange
				t.Name, // routing key
				false,  // mandatory
				false,
				amqp.Publishing{
					DeliveryMode: amqp.Persistent,
					ContentType:  "text/plain",
					Body:         []byte(fmt.Sprintf("%s", d.Body)),
				})
			failOnError(err, "Failed to publish a message")
			log.Printf(" [x] Sent %s to transcribe", fmt.Sprintf("%s", d.Body))

			d.Ack(false)
			dotCount := bytes.Count(d.Body, []byte("."))
			t := time.Duration(dotCount)
			time.Sleep(t * time.Second)
			log.Printf("Done")
		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever

}

// https://github.com/GoogleCloudPlatform/golang-samples/blob/master/storage/objects/main.go
func readFile(client *storage.Client, bucket, object string) ([]byte, error) {
	ctx := context.Background()
	// [START download_file]
	rc, err := client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return data, nil
	// [END download_file]
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

func transcode(filename string) (string, error) {

	var (
		cmdOut []byte
		err    error
	)

	// https://stackoverflow.com/questions/43890/crop-mp3-to-first-30-seconds
	if cmdOut, err = exec.Command("ffmpeg", "-t", "29", "-i", filename, "-vn", "-ac", "1", "-acodec", "pcm_s16le", "-ar", "16000", filename+".wav").Output(); err != nil {
		fmt.Printf(string(cmdOut))
		fmt.Fprintln(os.Stderr, "There was an error running ffmpeg: ", err)
		return "", err
		//os.Exit(1)
	}

	return string(cmdOut), nil

}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
		panic(fmt.Sprintf("%s: %s", msg, err))
	}
}
