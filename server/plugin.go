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
	// Register the slash command
	err := p.API.RegisterCommand(&model.Command{
		Trigger:          "approver",
		DisplayName:      "Approver Command",
		Description:      "A slash command for approval requests",
		AutoComplete:     true,
		AutoCompleteDesc: "Available commands: new",
		AutoCompleteHint: "[new]",
	})
	
	if err != nil {
		return err
	}

	// Register the API endpoints for handling form submissions
	p.API.RegisterInteractiveDialogHandler("newApprovalRequest", p.handleSubmitDialog)
	
	return nil
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

	// Split the command to get arguments
	splitCmd := strings.Fields(args.Command)
	if len(splitCmd) > 1 && splitCmd[1] == "new" {
		return p.handleNewCommand(args)
	}

	// Default response if no arguments or unrecognized arguments
	return &model.CommandResponse{
		ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
		Text:         "Hello, world! Use '/approver new' to open the approval form.",
	}, nil
}

// handleNewCommand opens a modal for creating a new approval request
func (p *Plugin) handleNewCommand(args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	// Create a new dialog
	dialog := model.Dialog{
		Title:       "New Approval Request",
		CallbackId:  "newApprovalRequest",
		SubmitLabel: "Submit",
		NotifyOnCancel: true,
		Elements: []model.DialogElement{
			{
				DisplayName: "Title",
				Name:        "title",
				Type:        "text",
				SubType:     "text",
				Optional:    false,
			},
			{
				DisplayName: "Description",
				Name:        "description",
				Type:        "text",
				SubType:     "textarea",
				Optional:    false,
			},
			{
				DisplayName: "Approver",
				Name:        "approver",
				Type:        "select",
				DataSource:  "users",
				Optional:    false,
			},
		},
	}

	// Show the dialog to the user
	request := model.OpenDialogRequest{
		TriggerId: args.TriggerId,
		URL:       fmt.Sprintf("/plugins/%s/api/v1/approvals/submit", manifest.Id),
		Dialog:    dialog,
	}

	if err := p.API.OpenInteractiveDialog(request); err != nil {
		return &model.CommandResponse{
			ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
			Text:         "Error opening the dialog: " + err.Error(),
		}, nil
	}

	return &model.CommandResponse{}, nil
}

// handleSubmitDialog processes the submitted dialog
func (p *Plugin) handleSubmitDialog(request *model.SubmitDialogRequest) (*model.SubmitDialogResponse, *model.AppError) {
	// Extract form values
	title := request.Submission["title"].(string)
	description := request.Submission["description"].(string)
	approverUserId := request.Submission["approver"].(string)
	
	// Send a direct message to the approver
	err := p.sendDirectMessage(request.UserId, approverUserId, title, description)
	if err != nil {
		return &model.SubmitDialogResponse{
			Error: "Failed to send message to approver: " + err.Error(),
		}, nil
	}
	
	// Send confirmation to the user who submitted the request
	p.API.SendEphemeralPost(request.ChannelId, &model.Post{
		UserId:    request.UserId,
		ChannelId: request.ChannelId,
		Message:   "Your approval request has been sent to the approver.",
	})
	
	return &model.SubmitDialogResponse{}, nil
}

// sendDirectMessage sends a direct message from one user to another
func (p *Plugin) sendDirectMessage(fromUserId, toUserId, title, description string) *model.AppError {
	// Get the direct channel between the users
	channel, appErr := p.API.GetDirectChannel(fromUserId, toUserId)
	if appErr != nil {
		return appErr
	}
	
	// Create the message with formatted content
	message := fmt.Sprintf("**%s**\n\n%s", title, description)
	
	// Create and send the post
	post := &model.Post{
		UserId:    fromUserId,
		ChannelId: channel.Id,
		Message:   message,
	}
	
	_, appErr = p.API.CreatePost(post)
	return appErr
}

func main() {
	plugin.ClientMain(&Plugin{})
}
