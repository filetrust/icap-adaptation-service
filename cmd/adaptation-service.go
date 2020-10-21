package main

import (
	"encoding/json"
	"log"
	"os"

	pod "github.com/icap-adaptation-service/pkg"
	"github.com/streadway/amqp"
)

var exchange = "adaptation-exchange"
var routingKey = "adaptation-request"
var queueName = "adaptation-request-queue"

func main() {
	podNamespace := os.Getenv("POD_NAMESPACE")
	amqpURL := os.Getenv("AMQP_URL")
	inputMount := os.Getenv("INPUT_MOUNT")
	outputMount := os.Getenv("OUTPUT_MOUNT")

	if podNamespace == "" || amqpURL == "" || inputMount == "" || outputMount == ""{
		log.Fatalf("init failed: POD_NAMESPACE, AMQP_URL, INPUT_MOUNT or OUTPUT_MOUNT environment variables not set")
	}

	conn, err := amqp.Dial(amqpURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	err = ch.ExchangeDeclare(exchange, "direct", true, false, false, false, nil)
	failOnError(err, "Failed to declare an exchange")

	q, err := ch.QueueDeclare(queueName, false, false, false, false, nil)
	failOnError(err, "Failed to declare a queue")

	err = ch.QueueBind(q.Name, routingKey, exchange, false, nil)
	failOnError(err, "Failed to bind queue")

	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	failOnError(err, "Failed to register a consumer")

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Printf("received a message: %s", d.Body)
			var body map[string]interface{}

			err := json.Unmarshal(d.Body, &body)
			if err != nil {
				ch.Nack(d.DeliveryTag, false, false)
				log.Printf("Failed to read message body, dropping message. Error: %s", err)
			}

			fileID := body["file-id"].(string)
			input := body["source-file-location"].(string)
			output := body["rebuilt-file-location"].(string)

			podArgs, err := pod.NewPodArgs(fileID, input, output, podNamespace, inputMount, outputMount, d.ReplyTo)
			if err != nil {
				ch.Nack(d.DeliveryTag, false, true)
				log.Printf("Failed to initialize Pod, placing message back on queue. Error: %s", err)
			}

			err = podArgs.CreatePod()
			if err != nil {
				ch.Nack(d.DeliveryTag, false, true)
				log.Printf("Unable to create pod, placing message back on queue. Error: %s", err)
			}
		}
	}()

	log.Printf("[*] Waiting for messages. To exit press CTRL+C")
	<-forever
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}
