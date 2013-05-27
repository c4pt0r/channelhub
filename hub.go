package main

import (
	"encoding/json"
	"github.com/garyburd/go-websocket/websocket"
	"github.com/gorilla/mux"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

const (
	writeWait      = 10 * time.Second
	readWait       = 60 * time.Second
	pingPeriod     = (readWait * 9) / 10
	maxMessageSize = 512
)

type User struct {
	userid string
	pic    string
}

type connection struct {
	channel string
	ws      *websocket.Conn
	send    chan []byte
	user    User
}

type controlRequest struct {
	cmd    string
	param  string
	result chan string
}

type message struct {
	msg_type string
	sender   string
	channel  string
	content  string
	date     time.Time
}

type hub struct {
	connections map[*connection]bool
	control     chan *controlRequest // for control messages
	broadcast   chan *message
	register    chan *connection
	unregister  chan *connection
}

var hmap = make(map[string]*hub)

func (u *User) String() string {
	m := map[string]string{
		"user": u.userid,
		"pic":  u.pic,
	}
	j, _ := json.Marshal(m)
	return string(j)
}

// 从websocket中读出信息，转发到指定channel中，然后广播给其他的人
func (c *connection) ReadPump(channelName string) {
	defer func() {
		hmap[channelName].unregister <- c
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(readWait))
	for {
		op, r, err := c.ws.NextReader()
		if err != nil {
			break
		}
		switch op {
		case websocket.OpPong:
			c.ws.SetReadDeadline(time.Now().Add(readWait))
		case websocket.OpText:
			rawmessage, err := ioutil.ReadAll(r)
			log.Printf("on msg arrival " + string(rawmessage) + " on channel:" + channelName)
			if err != nil {
				break
			}
			m := make(map[string]string)
			if err := json.Unmarshal([]byte(rawmessage), &m); err == nil {
				msg := &message{
					msg_type: m["type"],
					sender:   m["sender"],
					content:  m["content"],
					channel:  c.channel,
					date:     time.Now(),
				}

				hmap[channelName].broadcast <- msg
			}
		}
	}
}

// 从channel的拿到广播消息, 并写入当前connection的websocket中, 另外一个功能是心跳.
func (c *connection) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.write(websocket.OpClose, []byte{})
				return
			}
			if err := c.write(websocket.OpText, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.OpPing, []byte{}); err != nil {
				return
			}
		}
	}
}

func (c *connection) write(opCode int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(opCode, payload)
}

// 获取根据channelName获取channel对象
func GetChannel(channelName string) (*hub, error) {
	if _, ok := hmap[channelName]; !ok {
		hmap[channelName] = &hub{
			broadcast:   make(chan *message),
			register:    make(chan *connection),
			unregister:  make(chan *connection),
			connections: make(map[*connection]bool),
			control:     make(chan *controlRequest),
		}
		log.Printf("new channel: " + channelName + " run!")
		go hmap[channelName].Run()
	}
	return hmap[channelName], nil
}

func (h *hub) Broadcast(msg *message, filter func(c *connection) bool) {
	m := make(map[string]interface{})
	m["type"] = msg.msg_type
	m["sender"] = msg.sender
	m["content"] = msg.content
	m["channel"] = msg.channel
	m["date"] = msg.date

	data, _ := json.Marshal(m)
	log.Printf(string(data))
	for c := range h.connections {
		if filter != nil && filter(c) == false {
			continue
		}
		select {
		case c.send <- data:
		default:
			close(c.send)
			delete(h.connections, c)
		}

	}
}

// channel的消息fan-out在这里进行
func (h *hub) Run() {

	for {
		select {
		case req := <-h.control:
			if req.cmd == "onlineusers" {
				users := make([]string, 0)
				for k, _ := range h.connections {
					log.Printf("iter users!")
					users = append(users, k.user.String())
				}
				b, _ := json.Marshal(users)
				req.result <- string(b)
			}
		case c := <-h.register:
			h.connections[c] = true
			msg := &message{
				msg_type: "adduser",
				sender:   "sysadmin",
				content:  c.user.String(),
				channel:  c.channel,
				date:     time.Now(),
			}
			h.Broadcast(msg, nil)

		case c := <-h.unregister:
			msg := &message{
				msg_type: "removeuser",
				sender:   "sysadmin",
				content:  c.user.String(),
				channel:  c.channel,
				date:     time.Now(),
			}
			delete(h.connections, c)
			close(c.send)
			h.Broadcast(msg, nil)
		case m := <-h.broadcast:
			h.Broadcast(m, nil)
		}
	}
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Upgrade(w, r.Header, nil, 1024, 1024)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshake", 400)
		return
	} else if err != nil {
		log.Println(err)
		return
	}
	vars := mux.Vars(r)
	channelName := vars["channel"]
	// TODO: read user id from cookie
	if userid, err := readCookie("userid", r); err == nil {
		userpic, _ := readCookie("userpic", r)
		c := &connection{
			send:    make(chan []byte, 256),
			ws:      ws,
			channel: channelName,
			user:    User{userid, userpic},
		}
		channel, _ := GetChannel(channelName)
		channel.register <- c
		log.Printf("new connect arrival... channel name: " + string(channelName))
		go c.WritePump()
		c.ReadPump(channelName)
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}

}