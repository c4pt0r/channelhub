package main

import (
	"encoding/json"
	"github.com/garyburd/go-websocket/websocket"
	"github.com/gorilla/mux"
	"io/ioutil"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
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

type Message struct {
	MsgType string
	Sender  string
	Channel string
	Content string
	Date    int64
}

type hub struct {
	connections map[*connection]bool
	channelName string
	control     chan *controlRequest // for control messages
	broadcast   chan *Message
	register    chan *connection
	unregister  chan *connection
}

var hmap = make(map[string]*hub)

// mongo session
var mongoSession *mgo.Session
var mongoCollection *mgo.Collection

func init() {
	var err error
	if mongoSession, err = mgo.Dial(mongoServer); err == nil {
		mongoSession.SetMode(mgo.Monotonic, true)
		mongoCollection = mongoSession.DB("channelhub").C("messages")
	} else {
		mongoSession = nil
		mongoCollection = nil
	}
}

func (u *User) String() string {
	m := map[string]string{
		"user": u.userid,
		"pic":  u.pic,
	}
	j, _ := json.Marshal(m)
	return string(j)
}

func (u *User) Map() map[string]string {
	m := map[string]string{
		"user": u.userid,
		"pic":  u.pic,
	}
	return m
}

// 从websocket中读出信息，转发到指定channel中，然后广播给其他的人
func (c *connection) ReadPump(channelName string, req *http.Request) {
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
				if userid, err := ReadCookie("userid", req); err == nil {
					msg := &Message{
						MsgType: m["MsgType"],
						Sender:  userid,
						Content: m["Content"],
						Channel: c.channel,
						Date:    time.Now().Unix(),
					}
					// write to mongo
					mongoCollection.Insert(&msg)
					hmap[channelName].broadcast <- msg
				}
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
			broadcast:   make(chan *Message),
			register:    make(chan *connection),
			unregister:  make(chan *connection),
			connections: make(map[*connection]bool),
			control:     make(chan *controlRequest),
			channelName: channelName,
		}
		log.Printf("new channel: " + channelName + " run!")
		go hmap[channelName].Run()
	}
	return hmap[channelName], nil
}

func (h *hub) Broadcast(msg *Message, filter func(c *connection) bool) {
	m := make(map[string]interface{})
	m["MsgType"] = msg.MsgType
	m["Sender"] = msg.Sender
	m["Content"] = msg.Content
	m["Channel"] = msg.Channel
	m["Date"] = msg.Date

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
				users := make([]map[string]string, 0)
				usermap := make(map[string]bool)
				for k, _ := range h.connections {
					if _, ok := usermap[k.user.userid]; ok {
						continue
					}
					users = append(users, k.user.Map())
					usermap[k.user.userid] = true
				}
				b, _ := json.Marshal(users)
				req.result <- string(b)
			}
			if req.cmd == "history" {
				var messages []Message
				mongoCollection.Find(bson.M{"channel": h.channelName}).All(&messages)
				b, _ := json.Marshal(messages)
				req.result <- string(b)
			}
		case c := <-h.register:
			msg := &Message{
				MsgType: "adduser",
				Sender:  "sysadmin",
				Content: c.user.String(),
				Channel: c.channel,
				Date:    time.Now().Unix(),
			}
			// check if the user is now in room 
			flag := true
			for k, _ := range h.connections {
				if c.user.userid == k.user.userid {
					flag = false
					break
				}
			}
			if flag {
				log.Printf("new user coming: " + c.user.userid)
				h.Broadcast(msg, nil)
			}
			h.connections[c] = true
		case c := <-h.unregister:
			u_id := c.user.userid
			msg := &Message{
				MsgType: "removeuser",
				Sender:  "sysadmin",
				Content: c.user.String(),
				Channel: c.channel,
				Date:    time.Now().Unix(),
			}
			delete(h.connections, c)
			close(c.send)
			// check if the user is truly go away
			flag := true
			for k, _ := range h.connections {
				// if still in channel, do not broadcast remove message
				if u_id == k.user.userid {
					flag = false
					break
				}
			}
			if flag {
				log.Printf("user going away: " + c.user.userid)
				h.Broadcast(msg, nil)
			}
			if len(h.connections) == 0 {

			}
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
	if userid, err := ReadCookie("userid", r); err == nil {
		userpic, _ := ReadCookie("userpic", r)
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
		c.ReadPump(channelName, r)
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}
