package github

import (
	"testing"

	gogithub "github.com/google/go-github/v78/github"
)

func TestConvertPullRequestUsesRawPayload(t *testing.T) {
	connector := &Connector{}
	event := &gogithub.PullRequestEvent{
		Action: gogithub.Ptr("synchronize"),
		Repo: &gogithub.Repository{
			FullName: gogithub.Ptr("insmtx/lework"),
		},
		Sender: &gogithub.User{
			Login: gogithub.Ptr("octocat"),
		},
		PullRequest: &gogithub.PullRequest{
			Number:  gogithub.Ptr(12),
			Title:   gogithub.Ptr("Add Eino runtime"),
			HTMLURL: gogithub.Ptr("https://github.com/insmtx/lework/pull/12"),
			Head: &gogithub.PullRequestBranch{
				Ref: gogithub.Ptr("feature/eino"),
				SHA: gogithub.Ptr("abc123"),
			},
			Base: &gogithub.PullRequestBranch{
				Ref: gogithub.Ptr("main"),
				SHA: gogithub.Ptr("def456"),
			},
		},
		Installation: &gogithub.Installation{
			ID: gogithub.Ptr(int64(99)),
		},
	}

	converted := connector.convertPullRequest(event)
	if converted == nil {
		t.Fatalf("expected converted event")
	}
	if converted.EventType != EventTypePullRequest {
		t.Fatalf("unexpected event type: %s", converted.EventType)
	}
	if converted.Actor != "octocat" {
		t.Fatalf("unexpected actor: %s", converted.Actor)
	}
	if converted.Repository != "insmtx/lework" {
		t.Fatalf("unexpected repo: %s", converted.Repository)
	}
	if converted.Context["action"] != "synchronize" {
		t.Fatalf("unexpected context: %+v", converted.Context)
	}

	payload, ok := converted.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected raw payload map, got %#v", converted.Payload)
	}
	if payload["action"] != "synchronize" {
		t.Fatalf("unexpected payload action: %+v", payload)
	}
	pr, ok := payload["pull_request"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected pull_request payload: %+v", payload)
	}
	if pr["title"] != "Add Eino runtime" {
		t.Fatalf("unexpected pull_request payload: %+v", pr)
	}
	installation, ok := payload["installation"].(map[string]interface{})
	if !ok || installation["id"] != float64(99) {
		t.Fatalf("unexpected installation payload: %+v", payload)
	}
}

func TestConvertPullRequestSkipsUnsupportedAction(t *testing.T) {
	connector := &Connector{}
	event := &gogithub.PullRequestEvent{
		Action: gogithub.Ptr("closed"),
	}

	converted := connector.convertPullRequest(event)
	if converted != nil {
		t.Fatalf("expected unsupported pull request action to be skipped")
	}
}

func TestConvertPushUsesRawPayload(t *testing.T) {
	connector := &Connector{}
	event := &gogithub.PushEvent{
		Ref: gogithub.Ptr("refs/heads/main"),
		Repo: &gogithub.PushEventRepository{
			FullName: gogithub.Ptr("insmtx/lework"),
		},
		Sender: &gogithub.User{
			Login: gogithub.Ptr("sender-octocat"),
		},
		Pusher: &gogithub.CommitAuthor{
			Name: gogithub.Ptr("octocat"),
		},
		Commits: []*gogithub.HeadCommit{
			{
				Message: gogithub.Ptr("feat: add runtime"),
			},
		},
	}

	converted := connector.convertPush(event)
	if converted == nil {
		t.Fatalf("expected converted push event")
	}
	if converted.EventType != EventTypePush {
		t.Fatalf("unexpected event type: %s", converted.EventType)
	}
	if converted.Actor != "sender-octocat" {
		t.Fatalf("unexpected actor: %s", converted.Actor)
	}
	if converted.Repository != "insmtx/lework" {
		t.Fatalf("unexpected repository: %s", converted.Repository)
	}
	if converted.Context["ref"] != "refs/heads/main" {
		t.Fatalf("unexpected context: %+v", converted.Context)
	}
	if converted.Context["sender_login"] != "sender-octocat" {
		t.Fatalf("unexpected sender login in context: %+v", converted.Context)
	}

	payload, ok := converted.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected raw payload map, got %#v", converted.Payload)
	}
	commits, ok := payload["commits"].([]interface{})
	if !ok || len(commits) != 1 {
		t.Fatalf("unexpected commits payload: %+v", payload)
	}
	firstCommit, ok := commits[0].(map[string]interface{})
	if !ok || firstCommit["message"] != "feat: add runtime" {
		t.Fatalf("unexpected first commit payload: %+v", commits[0])
	}
}
