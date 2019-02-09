package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/google/go-github/v22/github"
	"github.com/prometheus/alertmanager/client"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/client_golang/api"
	"golang.org/x/oauth2"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	githubAPIURL           = flag.String("github-api-url", "https://api.github.com/", "api url of github")
	githubToken            = flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "token for github")
	githubOrg              = flag.String("github-org", "org", "org for github")
	githubTeam             = flag.String("github-team", "team", "team for github")
	githubDiscussionTitle  = flag.String("github-discussion-title", "Silence Overview", "title for the github discussion")
	githubAlertmanagerName = flag.String("github-alertmanager-name", "default", "title for the alertmanager block in the github discussion")
	alertmanagerAddr       = flag.String("alertmanager-addr", "http://localhost:9093", "Address of alertmanager to create/extend/delete silences")
	silenceCommentFilter   = flag.String("silence-comment-filter", "automated silence|silenced our tenants", "silences which comments contain this string are filtered out")
)

var githubTemplate = `
## {{ .Header }}

| Comment         | Creator           | Until          | Matchers          |
|-----------------|-------------------|----------------|-------------------|
{{- range $i, $t := .Silences }}
| {{ $t.Comment }} | {{ $t.CreatedBy }} | {{ $t.EndsAt.Format "2006-01-02" }} | ` + "`" + `{{ $t.ID }}` + "`" + ` |
{{- end }}

Last updated on {{ .LastUpdatedAt }}
`

func main() {
	flag.Parse()

	tc := oauth2.NewClient(context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: *githubToken},
	))

	githubClient := github.NewClient(tc)
	baseUrl, _ := url.Parse(*githubAPIURL)
	githubClient.BaseURL = baseUrl

	teams, _, err := githubClient.Teams.ListTeams(context.Background(), *githubOrg, &github.ListOptions{})
	if err != nil {
		panic(fmt.Errorf("error getting teams for org %s: %v", *githubOrg, err))
	}

	var team *github.Team
	for _, t := range teams {
		if t.GetSlug() == *githubTeam {
			team = t
		}
	}
	if team == nil {
		panic(fmt.Errorf("could not find team %s, only found teams %v", *githubTeam, teams))
	}

	discussions, _, err := githubClient.Teams.ListDiscussions(context.Background(), *team.ID, &github.DiscussionListOptions{})
	if err != nil {
		panic(fmt.Errorf("error listing discussions for team %s in org %s: %v", *team.Name, *githubOrg, err))
	}

	var discussion *github.TeamDiscussion
	var isNew bool
	for _, d := range discussions {
		if d.GetTitle() == *githubDiscussionTitle {
			discussion = d
		}
	}
	if discussion == nil {
		isNew = true
		pinned := true
		body := ""
		discussion = &github.TeamDiscussion{
			Title:  githubDiscussionTitle,
			Pinned: &pinned,
			Body: &body,
		}
	}

	if discussion.Body != nil && *discussion.Body != "" {
		removeOldAlertmanagerBlock(discussion.Body)
	}

	silences, err := getSilences()
	if err != nil {
		panic(err)
	}

	data := struct {
		Header        string
		Silences      []*types.Silence
		LastUpdatedAt string
	}{
		Header:        *githubAlertmanagerName,
		Silences:      silences,
		LastUpdatedAt: time.Now().Format(time.RFC3339),
	}

	var out bytes.Buffer
	err = template.Must(template.New("body").Parse(githubTemplate)).Execute(&out, data)
	if err != nil {
		panic(fmt.Errorf("error rendering template: %v", err))
	}

	// append new section
	body := fmt.Sprintf("%s\n%s\n%s\n%s", *discussion.Body, getStartIdentifier(), out.String(), getEndIdentifier())
	discussion.Body = &body

	if isNew {
		discussion, _, err = githubClient.Teams.CreateDiscussion(context.Background(), *team.ID, *discussion)
		if err != nil {
			panic(fmt.Errorf("error creating discussion %s in team %s in org %s: %v", *githubDiscussionTitle, *team.Name, *githubOrg, err))
		}
	} else {
		discussion, _, err = githubClient.Teams.EditDiscussion(context.Background(), *team.ID, *discussion.Number, *discussion)
		if err != nil {
			panic(fmt.Errorf("error editing discussion %s in team %s in org %s: %v", *githubDiscussionTitle, *team.Name, *githubOrg, err))
		}
	}
}

func getSilences() ([]*types.Silence, error) {

	c, err := api.NewClient(api.Config{Address: *alertmanagerAddr})
	if err != nil {
		return nil, fmt.Errorf("error creating Alertmanager client: %v", err)
	}

	silenceAPI := client.NewSilenceAPI(c)

	silences, err := silenceAPI.List(context.Background(), "")
	if err != nil {
		return nil, fmt.Errorf("error listing silences: %v", err)
	}

	regex := regexp.MustCompile(*silenceCommentFilter)

	var filteredSilences []*types.Silence
	for _, s := range silences {
		if !regex.MatchString(s.Comment) && !time.Now().After(s.EndsAt) {
			var matcher []string
			for _, m := range s.Matchers {
				matcher = append(matcher, m.String())
			}
			output := fmt.Sprintf("%v", matcher)
			s.ID = strings.Replace(output, "|", "\\|", -1)
			s.ID = strings.Replace(s.ID, "\"", "", -1)
			filteredSilences = append(filteredSilences, s)
		}
	}

	return filteredSilences, nil
}

const StartIdentifier = "<!-- START_%s -->"
const EndIdentifier = "<!-- END_%s -->"

func getStartIdentifier() string {
	return fmt.Sprintf(StartIdentifier, *githubAlertmanagerName)
}
func getEndIdentifier() string {
	return fmt.Sprintf(EndIdentifier, *githubAlertmanagerName)
}

func removeOldAlertmanagerBlock(body *string) {
	startIndex := strings.Index(*body, getStartIdentifier())
	endIndex := strings.Index(*body, getEndIdentifier())

	if startIndex != -1 && endIndex != -1 {
		oldBody := *body
		*body = oldBody[:startIndex-1] + oldBody[endIndex+len(getEndIdentifier()):]
	}
}
