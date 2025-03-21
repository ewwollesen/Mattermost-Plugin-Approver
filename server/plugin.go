package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

const (
	botUsername = "approvalbot"
	botDisplayName = "ApprovalBot"
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
	
	// Create or get the webhook for the bot
	botWebhookKey := "bot_webhook_id"
	webhookIDBytes, appErr := p.API.KVGet(botWebhookKey)
	
	if appErr != nil {
		return appErr
	}
	
	if webhookIDBytes == nil {
		// Create a new incoming webhook for the bot
		teamID := ""
		teams, appErr := p.API.GetTeams()
		if appErr != nil {
			return appErr
		}
		
		if len(teams) > 0 {
			teamID = teams[0].Id
		} else {
			return fmt.Errorf("no teams found to create webhook")
		}
		
		// Find a channel to attach the webhook to (we'll only use it for DMs)
		channels, appErr := p.API.GetChannelsForTeamForUser(teamID, "me", false)
		if appErr != nil || len(channels) == 0 {
			return fmt.Errorf("no channels found to create webhook")
		}
		
		hook := &model.IncomingWebhook{
			ChannelId:   channels[0].Id,
			TeamId:      teamID,
			DisplayName: botDisplayName,
			Description: "Webhook for the Approver plugin",
			Username:    botUsername,
		}
		
		createdHook, appErr := p.API.CreateIncomingWebhook(hook)
		if appErr != nil {
			return appErr
		}
		
		// Store the webhook ID
		if err := p.API.KVSet(botWebhookKey, []byte(createdHook.Id)); err != nil {
			return err
		}
	}
	
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
		URL:       fmt.Sprintf("/plugins/%s/dialog/submit", manifest.Id),
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

// sendDirectMessage sends a direct message from the bot to a user
func (p *Plugin) sendDirectMessage(fromUserId, toUserId, title, description string) *model.AppError {
	// Get the direct channel between the users
	channel, appErr := p.API.GetDirectChannel(fromUserId, toUserId)
	if appErr != nil {
		return appErr
	}
	
	// Get information about the requester
	requester, appErr := p.API.GetUser(fromUserId)
	if appErr != nil {
		return appErr
	}
	
	// Get the webhook ID
	webhookIDBytes, appErr := p.API.KVGet("bot_webhook_id")
	if appErr != nil {
		return appErr
	}
	
	if webhookIDBytes == nil {
		return &model.AppError{
			Message: "Bot webhook not found",
		}
	}
	
	// Create the message with formatted content
	message := fmt.Sprintf("**New Approval Request from @%s**\n\n**%s**\n\n%s", 
		requester.Username, title, description)
	
	// Get the webhook
	webhook, appErr := p.API.GetIncomingWebhook(string(webhookIDBytes))
	if appErr != nil {
		return appErr
	}
	
	// Create a post through the webhook
	webhookRequest := &model.IncomingWebhookRequest{
		ChannelId: channel.Id,
		Username:  botUsername,
		Text:      message,
	}
	
	// Use the API to post to the webhook
	if err := p.API.ExecuteIncomingWebhook(webhook.Id, webhookRequest); err != nil {
		return err
	}
	
	return nil
}

// ServeHTTP handles HTTP requests to the plugin
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	if path == "/dialog/submit" {
		p.handleDialogSubmission(w, r)
		return
	}
	
	http.NotFound(w, r)
}

// handleDialogSubmission processes the submitted dialog
func (p *Plugin) handleDialogSubmission(w http.ResponseWriter, r *http.Request) {
	request := model.SubmitDialogRequestFromJson(r.Body)
	if request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	// Extract form values
	title := request.Submission["title"].(string)
	description := request.Submission["description"].(string)
	approverUserId := request.Submission["approver"].(string)
	
	// Send a direct message to the approver
	err := p.sendDirectMessage(request.UserId, approverUserId, title, description)
	if err != nil {
		response := &model.SubmitDialogResponse{
			Error: "Failed to send message to approver: " + err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}
	
	// Send confirmation to the user who submitted the request
	p.API.SendEphemeralPost(request.ChannelId, &model.Post{
		UserId:    request.UserId,
		ChannelId: request.ChannelId,
		Message:   "Your approval request has been sent to the approver.",
	})
	
	response := &model.SubmitDialogResponse{}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func main() {
	plugin.ClientMain(&Plugin{})
}
