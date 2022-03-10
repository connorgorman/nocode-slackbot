package main

import (
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Workflow struct {
	Name string
	File string
}

type Config struct {
	SlackAppToken string `json:"slackAppToken"`
	SlackBotToken string `json:"slackBotToken"`
	DemoDir       string `json:"demoDir"`

	Workflows []*Workflow
}

func (c *Config) Validate() error {
	if c.SlackAppToken == "" {
		return errors.New("missing slack app token")
	}
	if !strings.HasPrefix(c.SlackAppToken, "xapp-") {
		return errors.New("slack app token should have xapp- prefix")
	}
	if c.SlackBotToken == "" {
		return errors.New("missing slack bot token")
	}
	if !strings.HasPrefix(c.SlackBotToken, "xoxb-") {
		return errors.New("slack bot token should have xoxb- prefix")
	}
	return nil
}

func main() {
	var configFile string
	cmd := &cobra.Command{
		Use:   "nocode-slackbot",
		Short: "A nocode way to make a slackbot",
		RunE: func(c *cobra.Command, args []string) error {
			data, err := os.ReadFile(configFile)
			if err != nil {
				return err
			}
			var config Config
			if err := yaml.Unmarshal(data, &config); err != nil {
				return err
			}
			if err := config.Validate(); err != nil {
				return err
			}

			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				if err := Run(&config); err != nil {
					panic(err)
				}
			}()

			sig := <-sigs
			log.Infof("Caught signal %v. Shutting down...", sig)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configFile, "config", "c", "config.yaml", "specify a config file to drive the demo")
	if err := cmd.Execute(); err != nil {
		log.Fatalf("error execution: %v", err)
	}
}
