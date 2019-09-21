package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/goji/httpauth"
	"github.com/gorilla/websocket"
)

const USERNAME string = "admin"
const PASSWORD string = "1234"
const DEBUG bool = true

type LogData struct {
	Type string
	Data string
	Time int64
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var newClientsChan chan chan LogData = make(chan chan LogData, 100)
var removedClientsChan chan chan LogData = make(chan chan LogData, 100)
var logsChan chan LogData = make(chan LogData, 100)

func main() {
	go func() {
		for {
			if DEBUG {
				fmt.Println("THREAD[1]: pm2 logs")
			}
			// cmd := exec.Command("ping", "-t", "google.com")
			cmd := exec.Command("pm2", "logs")
			cmdReader, err := cmd.StdoutPipe()
			if err != nil {
				log.Fatal(err)
			}
			scanner := bufio.NewScanner(cmdReader)
			if err := cmd.Start(); err != nil {
				log.Fatal(err)
			}
			for scanner.Scan() {
				data := scanner.Text()
				logsChan <- LogData{Type: "log", Data: data, Time: time.Now().UnixNano() / 1e6}
			}
			if err := scanner.Err(); err != nil {
				fmt.Println(err)
			}
		}
	}()

	go func() {
		for {
			if DEBUG {
				fmt.Println("THREAD[2]: pm2 jlist")
			}
			cmd := exec.Command("pm2", "jlist")
			data, err := cmd.Output()
			if err != nil {
				fmt.Println(err)
				continue
			}
			logsChan <- LogData{Type: "stats", Data: string(data), Time: time.Now().UnixNano() / 1e6}
			time.Sleep(10 * time.Second)
		}
	}()

	go func() {
		var clients map[chan LogData]bool = make(map[chan LogData]bool)
		for {
			select {
			case client := <-newClientsChan:
				if DEBUG {
					fmt.Println("added a new client")
				}
				clients[client] = true
			case client := <-removedClientsChan:
				if DEBUG {
					fmt.Println("removed a client")
				}
				delete(clients, client)
			case data := <-logsChan:
				for client := range clients {
					client <- data
				}
			}
		}
	}()

	http.Handle("/", httpauth.SimpleBasicAuth(USERNAME, PASSWORD)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("new http request: /")
		http.ServeFile(w, r, "./index.html")
	})))

	http.Handle("/logs", httpauth.SimpleBasicAuth(USERNAME, PASSWORD)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if DEBUG {
			fmt.Println("new http request: /logs")
		}
		var conn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			fmt.Println(err)
			return
		}
		clientChan := make(chan LogData, 100)

		newClientsChan <- clientChan

		for {
			select {
			case data := <-clientChan:
				if err := conn.WriteJSON(data); err != nil {
					conn.Close()
					removedClientsChan <- clientChan
					return
				}
			}
		}

	})))

	if err := http.ListenAndServe(":3030", nil); err != nil {
		fmt.Println(err)
	}
}
