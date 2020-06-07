package main

import (
	"log"
	"time"

	"rtclib"
)

type user struct {
	userid   string
	nickname string
	timer    *rtclib.Timer
}

func (u *user) subTimeout(d interface{}) {
	r := d.(*room)

	r.quit <- u
}

type room struct {
	name string            // roomid
	msgs chan *msg         // user Subscriber or Message
	resp chan *rtclib.JSIP // resp for request broadcast
	quit chan *user        // user quit room

	task *rtclib.Task

	// key: userid, value: user, record users register in rooms
	users map[string]*user
}

func (r *room) newUser(userid string, nickname string,
	expire time.Duration) *user {

	u := &user{
		userid:   userid,
		nickname: nickname,
	}

	u.timer = rtclib.NewTimer(expire, u.subTimeout, r)

	return u
}

func newRoom(name string, qsize int, subTimeout time.Duration,
	task *rtclib.Task) *room {

	r := &room{
		name: name,
		msgs: make(chan *msg, qsize),
		resp: make(chan *rtclib.JSIP, 1),
		quit: make(chan *user, qsize),

		task: task,

		users: make(map[string]*user),
	}

	return r
}

func (r *room) processSubscriber(sub *msg) {
	exp, _ := sub.req.GetInt("Expire")
	expire := time.Duration(exp) * time.Second
	userid, _ := sub.req.GetString("P-Asserted-Identity")

	user := r.users[userid]
	if user == nil {
		if expire > 0 { // User register in chatroom
			user = r.newUser(userid, sub.req.From, expire)
			r.users[userid] = user
			sub.res <- rtclib.JSIPMsgRes(sub.req, 200)
			log.Printf("User %s register in %s", userid, r.name)

			return
		}

		sub.res <- rtclib.JSIPMsgRes(sub.req, 404)
		log.Printf("User %s deregister, but not register in %s", userid, r.name)

		return
	}

	if expire > 0 { // User refresh register state in chatroom
		user.timer.Reset(expire)
	} else { // User deregister from chatroom
		delete(r.users, userid)
	}
}

func (r *room) processMessage(mess *msg) {
	userid, _ := mess.req.GetString("P-Asserted-Identity")

	if userid == "" {
		mess.res <- rtclib.JSIPMsgRes(mess.req, 404)
		log.Printf("%s receive msg but has no PAI", r.name)

		return
	}

	mess.res <- rtclib.JSIPMsgRes(mess.req, 200)

	for id, _ := range r.users {
		if id == userid {
			continue
		}

		message := rtclib.JSIPMsgClone(mess.req, r.task.NewDialogueID())
		message.RequestURI = id

		if len(message.Router) > 0 {
			message.Router = message.Router[1:]
		}

		message.SetString("P-Asserted-Identity", rtclib.Realm())

		rtclib.SendMsg(message)
	}
}

func (r *room) process() {
	for {
		select {
		case u := <-r.quit:
			delete(r.users, u.userid)

		case <-r.resp:
			return

		case msg := <-r.msgs:
			switch msg.req.Type {
			case rtclib.SUBSCRIBE:
				r.processSubscriber(msg)
			case rtclib.MESSAGE:
				r.processMessage(msg)
			}
		}
	}
}
