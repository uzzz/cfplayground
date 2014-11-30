package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/cloudfoundry-community/cfplayground/cf"
	. "github.com/cloudfoundry-community/cfplayground/commands"
	"github.com/cloudfoundry-community/cfplayground/config"
	"github.com/cloudfoundry-community/cfplayground/users"
	"github.com/cloudfoundry-community/cfplayground/websocket"
	"github.com/gorilla/mux"
)

type ServerHandlers interface {
	InitSession(http.ResponseWriter, *http.Request)
	RedirectBase(http.ResponseWriter, *http.Request)
	UploadHandler(http.ResponseWriter, *http.Request)
	DeleteHandler(http.ResponseWriter, *http.Request)
	BasePath() string
}

type Handlers struct {
	basePath string
}

func NewHandler(basePath string) ServerHandlers {
	return &Handlers{basePath}
}

func (h Handlers) InitSession(w http.ResponseWriter, r *http.Request) {
	pipe, err := websocket.New(w, r)

	if err != nil {
		panic("Failed to initialize websocket: " + err.Error())
	}

	token := users.GenerateToken()

	adminConfigs := readServerConfig()

	userConfigs, err := Admin_CreateNewUser(h.basePath, token, adminConfigs)
	if err != nil {
		fmt.Printf("\nFatal Error\n%s\n", err)
		os.Exit(1)
	}

	newCf := cf.NewCli(
		token,
		pipe.Out,
		pipe.In,
		pipe.Prompt,
		h.basePath,
		userConfigs,
	)

	user := users.New(
		w,
		r,
		h.basePath,
		token,
		newCf.(*cf.CF),
		pipe,
	)

	user.Pipe.Out <- &websocket.Message{"token", "", user.Token}
	go getConsoleInput(&user)
	CfLogin(&user)
}

func (h Handlers) RedirectBase(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, path.Join(h.basePath, "/ui"), http.StatusFound)
}

func (h Handlers) UploadHandler(w http.ResponseWriter, r *http.Request) {
	token := mux.Vars(r)["token"]

	os.MkdirAll(path.Join(users.List(token).CF.EnvVar(), "app"), os.ModePerm)

	//get the multipart reader for the request.
	reader, err := r.MultipartReader()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	//copy each part to destination.
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		//if part.FileName() is empty, skip this iteration.
		if part.FileName() == "" {
			continue
		}
		fmt.Println("file: ", part.FileName())
		dst, err := os.Create(path.Join(users.List(token).CF.EnvVar(), "app", part.FileName()))
		defer dst.Close()

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := io.Copy(dst, part); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func (h Handlers) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	token := mux.Vars(r)["token"]
	err := os.RemoveAll(filepath.Join("./containers/", token, "app"))
	if err != nil {
		fmt.Printf("Error deleting uploaded files for %s: %v \n", token, err)
	}
}

func (h Handlers) BasePath() string {
	return h.basePath
}

func readServerConfig() *config.Config {
	configs, err := config.New("./config/config.json")
	if err != nil {
		panic("Failed to read config file " + err.Error())
	}
	return configs
}
