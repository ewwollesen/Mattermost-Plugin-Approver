package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin
}

// OnActivate is invoked when the plugin is activated.
func (p *Plugin) OnActivate() error {
	return p.API.RegisterCommand(&model.Command{
		Trigger:          "approver",
		DisplayName:      "Approver Command",
		Description:      "A slash command that displays a hello world message",
		AutoComplete:     true,
		AutoCompleteDesc: "Type /approver to see a hello world message",
	})
}

// ExecuteCommand handles the /approver slash command
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	// Check if the command is /approver
	if !strings.HasPrefix(args.Command, "/approver") {
		return &model.CommandResponse{
			ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
			Text:         fmt.Sprintf("Unknown command: %s", args.Command),
		}, nil
	}

	// Return a message that only the user can see
	return &model.CommandResponse{
		ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
		Text:         "Hello, world!",
	}, nil
}

func main() {
	plugin.ClientMain(&Plugin{})
}
