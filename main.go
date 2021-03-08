package main

import (
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"html/template"
	"log"
	"net"
	"net/http"
	"sync"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

var udpHost = flag.String("udp_host", "127.0.0.1", "The udp log server address")
var udpPort = flag.Uint("udp_port", 10000, "The udp log server port")
var httpHost = flag.String("http_host", "127.0.0.1", "The http server address")
var httpPort = flag.Uint("http_port", 20000, "The http server port")

const IndexPage = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>udp log</title>
</head>
<body>
<p id="log-content"></p>
<script type="text/javascript">
    document.addEventListener("DOMContentLoaded", function () {
        let ws = new WebSocket("ws://{{ .Address }}:{{ .Port }}/log");
        ws.onopen = function () {
            console.log("connect to websocket server ok!");
            ws.send("client is ready to receive data!");
        };

        ws.onmessage = function (event) {
            let data = event.data;
            console.log("receive", data.length, "bytes data from server!");
            let container = document.getElementById("log-content");
            container.innerText += data;
        };

        ws.onerror = function (event) {
            console.log("something went error!");
        }

        ws.onclose = function () {
            console.log("close the connection!");
        }
    });
</script>
</body>
</html>`

func main() {
	flag.Parse()

	var wg sync.WaitGroup
	wg.Add(2)

	var connChan = make(map[*websocket.Conn]chan []byte)
	var mutex sync.Mutex

	// start up udp log server
	go func() {
		defer wg.Done()
		log.Println("Starting udp log server ...")

		udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", *udpHost, *udpPort))
		if err != nil {
			log.Println(err.Error())
			return
		}

		udpConn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			log.Println(err.Error())
			return
		}
		defer func() {
			_ = udpConn.Close()
		}()

		for {
			var buf [4096]byte

			n, err := udpConn.Read(buf[:])
			if err != nil {
				log.Println(err.Error())
				return
			}

			mutex.Lock()
			for _, logChan := range connChan {
				logChan <- buf[:n]
				log.Println("send", n, "bytes data to log channel")
			}
			mutex.Unlock()
		}
	}()

	// start up http server
	go func() {
		defer wg.Done()
		log.Println("Starting http server ...")

		// serve index page
		http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
			tmpl, err := template.New("index").Parse(IndexPage)
			if err != nil {
				log.Println(err.Error())
				return
			}

			err = tmpl.Execute(writer, map[string]interface{}{"Address": *httpHost, "Port": *httpPort})
			if err != nil {
				log.Println(err.Error())
				return
			}
		})

		// serve websocket server
		http.HandleFunc("/log", func(writer http.ResponseWriter, request *http.Request) {
			var upgrader = websocket.Upgrader{}
			conn, err := upgrader.Upgrade(writer, request, nil)
			if err != nil {
				log.Print("upgrade:", err)
				return
			}
			defer func() {
				_ = conn.Close()
			}()

			mt, message, err := conn.ReadMessage()
			if err != nil {
				log.Println(err.Error())
				return
			}
			log.Println("message type:", mt)
			log.Println(string(message))

			mutex.Lock()
			logChan := make(chan []byte, 4096)
			connChan[conn] = logChan
			mutex.Unlock()

			for {
				buf := <-logChan
				err := conn.WriteMessage(mt, buf)
				if err != nil {
					log.Println(err.Error())
					return
				}
			}
		})

		_ = http.ListenAndServe(fmt.Sprintf("%s:%d", *httpHost, *httpPort), nil)

	}()

	wg.Wait()
}
