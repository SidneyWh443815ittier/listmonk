package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
	flag "github.com/spf13/pflag"
)

const (
	appName    = "listmonk"
	appVersion = "dev"
)

// App is the global application state container.
type App struct {
	log    *log.Logger
	ko     *koanf.Koanf
	fs     stuffbin.FileSystem
}

var (
	// Injected at build time via ldflags.
	buildString = "unknown"
)

func main() {
	var (
		ko = koanf.New(".")
		l  = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Lshortfile)
	)

	// Define CLI flags.
	f := flag.NewFlagSet("config", flag.ContinueOnError)
	f.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", appName)
		f.PrintDefaults()
	}

	// Default to config.toml in the current directory, but also check ~/.listmonk/config.toml.
	// Personal note: I keep my config at ~/.listmonk/config.toml, so added it as a second default.
	f.StringSlice("config", []string{"config.toml", os.Getenv("HOME") + "/.listmonk/config.toml"},
		"path to one or more config files (will be merged in order)")
	f.Bool("install", false, "run first-time installation wizard")
	f.Bool("upgrade", false, "upgrade database to the latest schema")
	f.Bool("yes", false, "assume 'yes' to prompts during install/upgrade")
	f.Bool("version", false, "show current version of the build")
	f.Bool("new-config", false, "generate a new sample config.toml file")

	if err := f.Parse(os.Args[1:]); err != nil {
		l.Fatalf("error parsing flags: %v", err)
	}

	// Display version and exit.
	if ok, _ := f.GetBool("version"); ok {
		fmt.Printf("%s version %s | build: %s\n", appName, appVersion, buildString)
		os.Exit(0)
	}

	// Generate a new config file and exit.
	if ok, _ := f.GetBool("new-config"); ok {
		if err := generateNewConfig(); err != nil {
			l.Fatalf("error generating config: %v", err)
		}
		os.Exit(0)
	}

	// Load config files.
	cfgFiles, _ := f.GetStringSlice("config")
	for _, c := range cfgFiles {
		if err := ko.Load(file.Provider(c), toml.Parser()); err != nil {
			if os.IsNotExist(err) {
				// Warn but continue — missing optional config files are non-fatal.
				l.Printf("warning: config file not found, skipping: %s", c)
				continue
			}
			l.Fatalf("error loading config from file: %v", err)
		}
	}

	// Load environment variables (LISTMONK_ prefix).
	// Double underscores (__) are used as a delimiter for nested keys, e.g.
	// LISTMONK_DB__HOST maps to db.host in the config.
	if err := ko.Load(env.Provider("LISTMONK_", ".", func(s string) string {
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, "LISTMONK_")), "__", ".", -1)
	}), nil); err != nil {
		l.Fatalf("error loading config from env: %v", err)
	}

	// Override config with CLI flags.
	if err := ko.Load(posflag.Provider(f, ".", ko), nil); err != nil {
		l.Fatalf("error loading config from flags: %v", err)
	}

	// Initialize the app.
	app := &App{
		log: l,
		ko:  ko,
	}

	// Run the server.
	if err := initServer(app); err != nil {
		