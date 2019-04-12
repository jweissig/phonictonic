// FYI - I know about the hardcoded username, password, and ips here.
// this is part of the tutorial.
package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/streadway/amqp"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
		panic(fmt.Sprintf("%s: %s", msg, err))
	}
}

func main() {

	fmt.Println("booting worker_notify...")

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

	// define task_notify queue if it doesn't already exist
	q, err := ch.QueueDeclare(
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

			fmt.Println(task)

			// check to see if this was the last task, then send notify email
			time.Sleep(1 * time.Second)

			d.Ack(false)
			dotCount := bytes.Count(d.Body, []byte("."))
			t := time.Duration(dotCount)
			time.Sleep(t * time.Second)
			log.Printf("Send notify email for: %s", d.Body)
		}
	}()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever

}
