package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/cors"
	"github.com/urfave/negroni"
)

// main is tasked to bootstrap the service and notify of termination signals.
func main() {
	var s service
	s.configure()

	err := s.init()
	if err != nil {
		s.logger.Crit("initializing", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		signals := make(chan os.Signal, 2)
		signal.Notify(signals, os.Interrupt, os.Kill)
		<-signals
		cancel()
	}()

	s.run(ctx)
}

type service struct {
	// Configuration.
	bind             string
	descriptionsPath string

	// Dependencies
	logger       log15.Logger
	descriptions map[string]description
}

type description struct {
	Base     string  `json:"base"`
	Question block   `json:"question"`
	Answers  []block `json:"answers"`
}

type block struct {
	Size float64 `json:"size"`
	X    int     `json:"x"`
	Y    int     `json:"y"`
}

// configure read and validate the configuration of the service and populate
// the appropriate fields.
func (s *service) configure() {
	fs := flag.NewFlagSet("votrederniermot", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage of votrederniermot: votrederniermot [options]")
		fs.PrintDefaults()
	}

	// General options.
	fs.StringVar(&s.bind, "bind", "localhost:8080", "address to listen to")
	fs.StringVar(&s.descriptionsPath, "descriptions", "./descriptions.json", "")
	fs.Parse(os.Args[1:])
}

// init does the actual bootstraping of the service, once the configuration is
func (s *service) init() (err error) {
	s.logger = log15.New()
	s.logger.SetHandler(log15.StreamHandler(os.Stdout, log15.LogfmtFormat()))

	// Parse the descriptions.
	raw, err := ioutil.ReadFile(s.descriptionsPath)
	if err != nil {
		return wrap(err, "reading descriptions file")
	}

	err = json.Unmarshal(raw, &s.descriptions)
	if err != nil {
		return wrap(err, "parsing descriptions file")
	}

	return nil
}

// run does the actual running of the service until the context is closed.
func (s *service) run(ctx context.Context) {
	s.logger.Debug("registering routes")
	router := httprouter.New()
	router.NotFound = http.HandlerFunc(s.notFound)
	router.MethodNotAllowed = http.HandlerFunc(s.methodNotAllowed)
	router.GET("/", s.root)

	s.logger.Debug("registering middlewares")
	stack := negroni.New()
	stack.Use(negroni.NewRecovery())
	stack.Use(negroni.HandlerFunc(s.logRequest))
	stack.Use(cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete},
	}))
	stack.UseHandler(router)

	s.logger.Debug("starting server")
	server := &http.Server{
		Addr:    s.bind,
		Handler: stack,
	}
	go func() {
		<-ctx.Done()
		ctx, _ := context.WithTimeout(ctx, 1*time.Minute)
		server.Shutdown(ctx)
	}()
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("closing server", "err", err)
	}
	s.logger.Info("stopping server")
}

// Log a request with a few metadata to ensure requests are monitorable.
func (s *service) logRequest(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	start := time.Now()

	next(rw, r)

	res := rw.(negroni.ResponseWriter)
	s.logger.Info("request",
		"started_at", start,
		"duration", time.Since(start),
		"method", r.Method,
		"path", r.URL.Path,
		"status", res.Status(),
	)
}

func (s *service) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, fmt.Errorf(`endpoint %q not found`, r.URL.Path))
}

func (s *service) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf(`method %q not allowed for endpoint %q`, r.Method, r.URL.Path))
}

func (s *service) root(rw http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	req := generateRequest{
		r:            r,
		logger:       s.logger,
		descriptions: s.descriptions,
	}
	req.init()
	req.readPayload()
	req.getBase()
	req.getFont()
	req.writeQuestion()
	req.writeAnswers()
	if req.err != nil {
		writeError(rw, http.StatusInternalServerError, req.err)
	}
	png.Encode(rw, req.image)
}

// wrap an error using the provided message and arguments.
func wrap(err error, msg string, args ...interface{}) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(msg, args...), err)
}

// write a payload and a status to the ResponseWriter.
func write(w http.ResponseWriter, status int, payload interface{}) {
	w.WriteHeader(status)
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(raw)
}

// write an error and a status to the ResponseWriter.
func writeError(w http.ResponseWriter, status int, err error) {
	write(w, status, Error{Err: err.Error()})
}

// Error type for API return values.
type Error struct {
	Err string `json:"error"`
}

// read a payload from a request body.
func read(r *http.Request, dest interface{}) error {
	raw, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return wrap(err, "reading body")
	}

	return json.Unmarshal(raw, dest)
}
