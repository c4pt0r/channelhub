package main

import (
    "github.com/gorilla/mux"
    "net/http"
)

func onlineUsersHandler(w http.ResponseWriter, r * http.Request) {
    vars := mux.Vars(r)
    channelName := vars["channel"]
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    if res, err := RunCmd(channelName, "onlineusers", ""); err == nil {
        w.Write([]byte(res))
        return
    }
    http.Error(w,"server error", 500)
    return
}

func logoutHandler(w http.ResponseWriter, r * http.Request) {
    SetCookie("userid", "", w)
    http.Redirect(w, r, "/", http.StatusFound)
}
