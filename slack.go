package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

var (
	messages = make(map[string]*slack.Message)

	finishedWorkflows     []*FinishedWorkflow
	finishedWorkflowsLock sync.RWMutex
)

type FinishedWorkflow struct {
	User    string
	Time    time.Time
	Message string
	Value   string
}

func simplePayload(text string) map[string]interface{} {
	return map[string]interface{}{
		"blocks": []slack.Block{
			slack.NewSectionBlock(
				&slack.TextBlockObject{
					Type: slack.MarkdownType,
					Text: text,
				},
				nil,
				nil,
			),
		},
	}
}

func handleSlashCommand(client *socketmode.Client, evt *socketmode.Event, workflows map[string]*Workflow) {
	cmd, ok := evt.Data.(slack.SlashCommand)
	if !ok {
		return
	}

	switch cmd.Command {
	case "/summary":
		var blocks []slack.Block
		for _, workflow := range finishedWorkflows {
			blocks = append(blocks, slack.NewSectionBlock(
				&slack.TextBlockObject{
					Type: slack.MarkdownType,
					Text: fmt.Sprintf("%s | %s | selected %s", workflow.User, workflow.Time.Format(time.RFC3339), workflow.Value),
				},
				nil,
				nil,
			))
		}
		payload := map[string]interface{}{
			"blocks": blocks,
		}
		client.Ack(*evt.Request, payload)
	case "/workflow":
		workflow, ok := workflows[cmd.Text]
		if !ok {
			payload := simplePayload(fmt.Sprintf("could not find workflow %s", cmd.Text))
			client.Ack(*evt.Request, payload)
			return
		}
		msg, ok := messages[workflow.File]
		if !ok {
			client.Debugf("Message %s is not available", workflow.File)
			return
		}
		client.Ack(*evt.Request, msg)
	}
}

func Run(config *Config) error {
	api := slack.New(
		config.SlackBotToken,
		slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stdout, "api: ", log.Lshortfile|log.LstdFlags)),
		slack.OptionAppLevelToken(config.SlackAppToken),
	)

	client := socketmode.New(
		api,
		socketmode.OptionDebug(true),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	entries, err := os.ReadDir(config.DemoDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		file, err := os.ReadFile(filepath.Join(config.DemoDir, entry.Name()))
		if err != nil {
			return err
		}

		var msg slack.Message
		if err := json.Unmarshal(file, &msg); err != nil {
			return err
		}
		messages[entry.Name()] = &msg
	}

	workflows := make(map[string]*Workflow)
	for _, w := range config.Workflows {
		workflows[w.Name] = w
	}

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				fmt.Println("Connecting to Slack with Socket Mode...")
			case socketmode.EventTypeConnectionError:
				fmt.Println("Connection failed. Retrying later...")
			case socketmode.EventTypeConnected:
				fmt.Println("Connected to Slack with Socket Mode.")
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				fmt.Printf("Event received: %+v\n", eventsAPIEvent)
				client.Ack(*evt.Request)
			case socketmode.EventTypeInteractive:
				callback, ok := evt.Data.(slack.InteractionCallback)
				if !ok {
					continue
				}
				if callback.Type == slack.InteractionTypeBlockActions {
					finishedWorkflowsLock.Lock()
					finishedWorkflows = append(finishedWorkflows, &FinishedWorkflow{
						User:  callback.User.Name,
						Time:  time.Now(),
						Value: callback.ActionCallback.BlockActions[0].Value,
					})
					finishedWorkflowsLock.Unlock()

					client.Debugf(callback.ActionCallback.BlockActions[0].Value)

					msg, ok := messages[callback.ActionCallback.BlockActions[0].ActionID]
					if !ok {
						client.Debugf("could not find %+v", messages)
						payload := simplePayload(fmt.Sprintf("could not find message callback %s", callback.ActionCallback.BlockActions[0].Value))
						client.Ack(*evt.Request, payload)
						continue
					}
					client.Ack(*evt.Request, struct{}{})
					if _, _, err := client.PostMessage(callback.Channel.ID, slack.MsgOptionBlocks(msg.Blocks.BlockSet...)); err != nil {
						client.Debugf("error sending message: %v", err)
					}
					continue
				}
				var payload interface{}
				client.Ack(*evt.Request, payload)
			case socketmode.EventTypeSlashCommand:
				handleSlashCommand(client, &evt, workflows)
			default:
				fmt.Fprintf(os.Stderr, "Unexpected event type received: %s\n", evt.Type)
			}
		}
	}()

	return client.Run()
}
