package main

import (
	"github.com/gorilla/mux"
	"net/http"
)

func onlineUsersHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channelName := vars["channel"]
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if res, err := RunCmd(channelName, "onlineusers", ""); err == nil {
		w.Write([]byte(res))
		return
	}
	http.Error(w, "server error", 500)
	return
}

func getChannelList(w http.ResponseWriter, r *http.Request) {
	for k, _ := range hmap {
	}
	return
}

func setEnteringStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channelName := vars["channel"]
	status := r.FormValue("status")
	if userid, err := ReadCookie("userid", r); err == nil && status != nil {
		// TODO broadcast to other users
	}
	return
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	SetCookie("userid", "", w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func historyHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channelName := vars["channel"]

	if res, err := RunCmd(channelName, "history", ""); err == nil {
		w.Write([]byte(res))
		return
	}
	http.Error(w, "server error", 500)
	return
}
