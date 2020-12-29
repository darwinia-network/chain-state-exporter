package main

import (
	"fmt"
	"net/http"
	"os"

	flags "github.com/jessevdk/go-flags"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var opts struct {
	LogLevel            uint32 `long:"log-level" description:"The log level (0 ~ 6), use 5 for debugging, see https://pkg.go.dev/github.com/sirupsen/logrus#Level" value-name:"N" default:"4"`
	Listen              string `long:"listen" description:"Exporter listen address" value-name:"[ADDR]:PORT" default:":9602"`
	WebsocketEndpoint   string `short:"e" long:"ws-endpoint" description:"Darwinia node websocket endpoint" value-name:"ws|wss://" default:"ws://127.0.0.1:9944"`
	MetricsPath         string `long:"metrics-path" description:"Exposed metrics path" value-name:"PATH" default:"/metrics"`
	CustomTypesFilePath string `long:"types-file" description:"Path to the custom types file" default:"types.json"`
}

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

var exporter *Exporter

func scrapeHandler(w http.ResponseWriter, r *http.Request) {
	promhttp.HandlerFor(
		exporter.registry, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError},
	).ServeHTTP(w, r)
}

func main() {
	if _, err := flags.Parse(&opts); err != nil {
		os.Exit(0)
	}

	fmt.Printf("Chain State Exporter %v-%v (built %v)\n", buildVersion, buildCommit, buildDate)
	logrus.SetLevel(logrus.Level(opts.LogLevel))

	var err error
	exporter, err = NewExporter(opts.WebsocketEndpoint, opts.CustomTypesFilePath)
	if err != nil {
		logrus.Fatal(err)
	}

	http.HandleFunc(opts.MetricsPath, scrapeHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`<html>
<head><title>Chain State Exporter</title></head>
<body>
<h1>Chain State Exporter ` + buildVersion + `</h1>
<p><a href='` + opts.MetricsPath + `'>Metrics</a></p>
</body>
</html>
`))
		if err != nil {
			logrus.Debugf("Write() err: %s", err)
		}
	})

	logrus.Infof("Server is ready to handle incoming scrape requests.")
	logrus.Fatal(http.ListenAndServe(opts.Listen, nil))
}
