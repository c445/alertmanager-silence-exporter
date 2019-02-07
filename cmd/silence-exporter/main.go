package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/google/go-github/v22/github"
	"github.com/prometheus/alertmanager/client"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/client_golang/api"
	"golang.org/x/oauth2"
	"html/template"
	"net/url"
	"strings"
	"time"
)

var (
	//TODO remove token
	githubToken            = flag.String("github-token", "", "token for github")
	githubOrg              = flag.String("github-org", "c445", "org for github")
	githubTeam             = flag.String("github-team", "core-platform", "team for github")
	githubDiscussionTitle  = flag.String("github-discussion-title", "Silence Overview", "title for the github discussion")
	githubAlertmanagerName = flag.String("github-alertmanager-name", "c01p005", "title for the alertmanager block in the github discussion")
	alertmanagerAddr       = flag.String("alertmanager-addr", "http://localhost:9093", "Address of alertmanager to create/extend/delete silences")
	silenceCommentFilter   = flag.String("silence-comment-filter", "automated silence", "silences which comments contain this string are filtered out")
)

var githubTemplate = `
## {{ .Header }}

| Comment         | Creator           | Until          | Matchers          |
|-----------------|-------------------|----------------|-------------------|
{{- range $i, $t := .Silences }}
| {{ $t.Comment }} | {{ $t.CreatedBy }} | {{ $t.EndsAt }} | {{ $t.Matchers }}  |
{{- end }}

Last updated on {{ .LastUpdatedAt }}
`

func main() {
	flag.Parse()

	tc := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: *githubToken},
	))

	githubClient := github.NewClient(tc)
	baseUrl, _ := url.Parse("https://git.daimler.com/api/v3/")
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
		discussion = &github.TeamDiscussion{
			Title:  githubDiscussionTitle,
			Pinned: &pinned,
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

	body := fmt.Sprintf("%s\n%s\n%s", getStartIdentifier(), out.String(), getEndIdentifier())
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

	var filteredSilences []*types.Silence
	for _, s := range silences {
		if !strings.Contains(s.Comment, *silenceCommentFilter) {
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
		*body = oldBody[:startIndex] + oldBody[endIndex:]
	}
}
