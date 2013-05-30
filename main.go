package main

import (
    "github.com/msbranco/goconfig"
    "code.google.com/p/goauth2/oauth"
    "encoding/json"
    "errors"
    "flag"
    "github.com/gorilla/mux"
    "github.com/gorilla/securecookie"
    "io/ioutil"
    "log"
    "net/http"
    "text/template"
    "time"
)

var addr = flag.String("addr", ":8080", "http service address")
var homeTempl = template.Must(template.ParseFiles("templates/index.html"))
var chatTempl = template.Must(template.ParseFiles("templates/chat.html"))

// run control on channel
func RunCmd(channelName string, cmd string, param string) (result string, err error) {
    h, _ := GetChannel(channelName)
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    resultChan := make(chan string)
    h.control <- &controlRequest{
        cmd:    cmd,
        param:  param,
        result: resultChan,
    }
    for {
        select {
        case result = <-resultChan:
            err = nil
            return
        case <-ticker.C:
            result = ""
            err = errors.New("request timeout")
            return
        }
    }
    return
}

var config = &oauth.Config{
    ClientId:     "54146317888.apps.googleusercontent.com",
    ClientSecret: "YyuLo4vNT04N36nnl8e3XcV-",
    Scope:        "https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/userinfo.email",
    RedirectURL:  "http://localhost:8080/oauth2callback",
    AuthURL:      "https://accounts.google.com/o/oauth2/auth",
    TokenURL:     "https://accounts.google.com/o/oauth2/token",
    //TokenCache:   oauth.CacheFile(*cachefile),
}

var mongoServer string

func readConfigFile(section string) {
    if c, err := goconfig.ReadConfigFile("conf.cfg"); err == nil {
        clientId, _ := c.GetString(section, "clientid")
        clientSecret, _ := c.GetString(section, "clientsecret")
        redirectURL, _ := c.GetString(section, "redirect")
        mongoServer, _ = c.GetString(section, "mongo")

        config.ClientId = clientId
        config.ClientSecret = clientSecret
        config.RedirectURL = redirectURL
    }
}

//  secret!
var hashKey = []byte("1234567890123456")
var blockKey = []byte("1234567890123456")
var scookie = securecookie.New(hashKey, blockKey)

func SetCookie(k string, v string, w http.ResponseWriter) (err error) {
    if encoded, err := scookie.Encode(k, v); err == nil {
        cookie := &http.Cookie{
            Name:  k,
            Value: encoded,
            Path:  "/",
        }
        log.Printf(encoded)
        http.SetCookie(w, cookie)
    } else {
        log.Printf(err.Error())
    }
    return err
}

func ReadCookie(k string, r *http.Request) (string, error) {
    if cookie, err := r.Cookie(k); err == nil {
        var value string
        if err = scookie.Decode(k, cookie.Value, &value); err == nil {
            if value == "" {
                return "", errors.New("no such cookie")
            }
            return value, nil
        }
    }
    return "", errors.New("no such cookie")
}

func serveHome(w http.ResponseWriter, r *http.Request) {
    url := config.AuthCodeURL("")
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    if logonId, err := ReadCookie("userid", r); err == nil {
        w.Write([]byte(logonId))
        return
    } else {
        homeTempl.Execute(w, url)
        return
    }
}

func chatHome(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")

    if _, err := ReadCookie("userid", r); err != nil {
        http.Redirect(w, r, "/", http.StatusFound)
        return
    }

    vars := mux.Vars(r)
    channelName := vars["channel"]

    data := struct {
        ChannelName string
        Host        string
    }{
        channelName,
        r.Host,
    }

    chatTempl.Execute(w, data)
}

func oauth2Handler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    code := r.FormValue("code")
    transport := &oauth.Transport{Config: config}

    token, err := transport.Exchange(code)
    transport.Token = token
    req, err := transport.Client().Get("https://www.googleapis.com/oauth2/v1/userinfo")
    if err != nil {
        http.Error(w, "token invilid", 500)
        return
    }
    defer r.Body.Close()
    content, err := ioutil.ReadAll(req.Body)
    m := make(map[string]string)
    json.Unmarshal([]byte(content), &m)

    userid := m["email"]
    usericon := m["picture"]

    SetCookie("userid", userid, w)
    SetCookie("userpic", usericon, w)

    http.Redirect(w, r, "/", http.StatusFound)
}
func main() {
    flag.Parse()
    readConfigFile("default")
    log.Printf(config.ClientId)
    log.SetFlags(log.Lshortfile | log.LstdFlags)

    r := mux.NewRouter()
    r.HandleFunc("/", serveHome)
    r.HandleFunc("/oauth2callback", oauth2Handler)
    r.HandleFunc("/logout", logoutHandler)
    r.HandleFunc("/{channel}", chatHome)
    r.HandleFunc("/{channel}/ws", serveWs)
    r.HandleFunc("/{channel}/online", onlineUsersHandler)
    r.HandleFunc("/{channel}/history", historyHandler)

    //r.HandleFunc("/login", loginHandler)

    r.Handle("/favicon.ico", http.FileServer(http.Dir("statics/")))
    r.PathPrefix("/static/").Handler(http.StripPrefix("/static/",
        http.FileServer(http.Dir("static/"))))
    http.Handle("/", r)
    http.ListenAndServe(":8080", r)
}
