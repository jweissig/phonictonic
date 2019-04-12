// FYI - I know about the hardcoded username, password, and ips here.
// this is part of the tutorial.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/streadway/amqp"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"

	"golang.org/x/net/context"

	speech "cloud.google.com/go/speech/apiv1p1beta1"
	"cloud.google.com/go/storage"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1p1beta1"
	longrunningpb "google.golang.org/genproto/googleapis/longrunning"
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
		panic(fmt.Sprintf("%s: %s", msg, err))
	}
}

func main() {

	fmt.Println("booting worker_transcribe...")

	// sql db connection
	db, err := gorm.Open("mysql", "phonic-prod:qCCgQKIJg9FWU0ox@tcp(127.0.0.1:3306)/phonic_prod?charset=utf8&parseTime=True&loc=UTC")

	// check for error
	if err != nil {
		//panic("Failed to connect database (is the container started?).")
		log.Println(err)
	} else {
		log.Println("connected to mysql!")
	}

	// config db logger
	db.LogMode(true)

	// connect to rabbit mq
	conn, err := amqp.Dial("amqp://B006DCF4:788A2F817EF9@35.000.000.000:5672/")
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	// define task_transcode queue if it doesn't already exist
	q, err := ch.QueueDeclare(
		"task_transcribe", // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	failOnError(err, "Failed to declare a queue")

	// define task_notify queue if it doesn't already exist
	n, err := ch.QueueDeclare(
		"task_notify", // name
		true,          // durable
		false,         // delete when unused
		false,         // exclusive
		false,         // no-wait
		nil,           // arguments
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

			// parse message job/task
			message := strings.Split(fmt.Sprintf("%s", d.Body), "/")
			jobID, taskID := message[0], message[1]
			log.Printf("Received a message: %s/%s", jobID, taskID)

			// get db record
			var task Task
			err := db.First(&task, "task_uuid = ?", taskID).Error
			if err != nil {
				log.Printf("Error fetching task: %s", err)
			}

			// set status to transcribing
			task.Status = "Transcribing"
			err = db.Save(&task).Error
			if err != nil {
				log.Printf("Error updating task: %s", err)
			}

			// connect to google speech api
			ctx := context.Background()
			client, err := speech.NewClient(ctx)
			if err != nil {
				log.Fatal(err)
			}

			log.Printf("Sending request to google speech API: %s", d.Body)
			opName, err := sendGCS(client, fmt.Sprintf("gs://phonic-test-staging/%s.wav", d.Body))
			if err != nil {
				log.Fatal(err)
			}

			log.Printf("Waiting for reply from google speech API: %s", d.Body)
			resp, err := wait(client, opName)
			if err != nil {
				log.Fatal(err)
			}

			//spew.Dump(resp)

			// parse response
			respJSON, _ := json.Marshal(resp)

			// save to storage
			err = ioutil.WriteFile("./staging/"+taskID+".json", respJSON, 0644)
			if err != nil {
				log.Fatal(err)
			}

			// create client to google storage
			gcsctx := context.Background()
			gcsclient, err := storage.NewClient(gcsctx)
			if err != nil {
				log.Fatal(err)
			}

			// upload transcode media to GCS
			if err = uploadFile(gcsclient, "phonic-test-staging", jobID, fmt.Sprintf("%s.json", taskID)); err != nil {
				log.Fatalf("Cannot write object: %v", err)
			}
			log.Printf("Uploaded to Google Storage: %s.json", d.Body)

			// update database with status & json blob
			task.Status = "Completed"
			task.JSONRaw = string(respJSON)
			err = db.Save(&task).Error
			if err != nil {
				log.Printf("Error updating task: %s", err)
			}

			// clean up and remove the json file from the local filesystem
			err = os.Remove("./staging/" + taskID + ".json")
			if err != nil {
				fmt.Println(err)
			}

			// add notify message into the queue
			err = ch.Publish(
				"",     // exchange
				n.Name, // routing key
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
			log.Printf("Completed google speech API: %s", d.Body)
		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever

}

func wait(client *speech.Client, opName string) (*speechpb.LongRunningRecognizeResponse, error) {
	ctx := context.Background()

	opClient := longrunningpb.NewOperationsClient(client.Connection())
	var op *longrunningpb.Operation
	var err error

	//fmt.Print("Waiting")

	for {
		op, err = opClient.GetOperation(ctx, &longrunningpb.GetOperationRequest{
			Name: opName,
		})

		//spew.Dump(op)

		if err != nil {
			return nil, err
		}
		if op.Done {
			//fmt.Println()
			break
		}

		//fmt.Print(".")

		time.Sleep(5 * time.Second)
	}

	switch {
	case op.GetError() != nil:
		return nil, fmt.Errorf("recieved error in response: %v", op.GetError())
	case op.GetResponse() != nil:
		var resp speechpb.LongRunningRecognizeResponse
		if err := proto.Unmarshal(op.GetResponse().Value, &resp); err != nil {
			return nil, err
		}
		//spew.Dump(op)
		//spew.Dump(&resp)
		return &resp, nil
	}

	// should never happen.
	return nil, errors.New("no response")
}

func sendGCS(client *speech.Client, gcsURI string) (string, error) {
	ctx := context.Background()

	// Send the contents of the audio file with the encoding and
	// and sample rate information to be transcripted.
	req := &speechpb.LongRunningRecognizeRequest{
		Config: &speechpb.RecognitionConfig{
			Encoding:                 speechpb.RecognitionConfig_LINEAR16,
			SampleRateHertz:          16000,
			LanguageCode:             "en-US",
			MaxAlternatives:          0,
			EnableSpeakerDiarization: true,
			Model: "video",
			EnableWordTimeOffsets:      true,
			EnableWordConfidence:       true,
			EnableAutomaticPunctuation: true,
		},
		Audio: &speechpb.RecognitionAudio{
			AudioSource: &speechpb.RecognitionAudio_Uri{Uri: gcsURI},
		},
	}

	//spew.Dump(req)

	op, err := client.LongRunningRecognize(ctx, req)
	if err != nil {
		return "", err
	}
	return op.Name(), nil
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
