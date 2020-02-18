package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TODO: make sure we don't panic on a nil field

type EventFormatterOptions struct {
	Branches string
	NoColors bool
	LongURL  bool
}

type GHEvent struct {
	Type       string
	Sender     GHSender
	Repository GHRepository
	//Organization

	// Push event
	// https://developer.github.com/v3/activity/events/types/#pushevent
	Ref     string
	After   string
	Before  string
	Created bool
	Deleted bool
	Forced  bool
	BaseRef string `json:"base_ref"`
	Compare string
	Commits []GHCommit
	Pusher  GHPusher

	// Pull request event
	PullRequest *GHPullRequest `json:"pull_request"`

	// Issues event & Issue comment event
	// https://developer.github.com/v3/activity/events/types/#issuesevent
	// https://developer.github.com/v3/activity/events/types/#issuecommentevent
	Action  string
	Issue   *GHIssue
	Comment *GHComment

	// Gollum event
	Pages []GHPage

	// TODO: star
	// TODO: ping
}

type GHSender struct {
	Login string
	// ...
}

type GHRepository struct {
	Name     string
	FullName string `json:"full_name"`
	Private  bool
	Owner    GHOwner
	URL      string
	// ...
}

type GHOwner struct {
	Name  string
	Login string
}

type GHCommit struct {
	SHA      string
	Message  string
	Author   GHAuthor
	URL      string
	Distinct bool

	ID string // XXX ???
}

type GHAuthor struct {
	Name  string
	Email string
}

type GHPusher struct {
	Name string
}

type GHPullRequest struct {
	Number  int
	Title   string
	HtmlUrl string `json:"html_url"`
	Head    GHPRBranch
	Base    GHPRBranch
}

type GHPRBranch struct {
	Label string
	Ref   string
	SHA   string
}

type GHIssue struct {
	Number  int
	Title   string
	HtmlUrl string `json:"html_url"`
	// ...
}

type GHComment struct {
	Body     string
	CommitID string `json:"commit_id"`
	HtmlUrl  string `json:"html_url"`
}

type GHPage struct {
	Action  string
	Title   string
	Summary string
	HtmlUrl string `json:"html_url"`
}

// Parse a JSON payload containing a github event,
// and return a GHEvent object.
func ParseGithubEvent(b []byte) (*GHEvent, error) {
	event := new(GHEvent)
	err := json.Unmarshal(b, event)
	return event, err
}

// TODO: FilterGithubEvent

// Format a github event according to the event type,
// in a manner suitable for transmitting via irc.
// May return multiple lines.
// May return an empty string if the event should be ignored.
// The cfg parameter can be nil, in which case the default options will be used.
func FormatGithubEvent(eventType string, event *GHEvent, cfg *EventFormatterOptions) string {
	if cfg == nil {
		cfg = &EventFormatterOptions{
			LongURL:  true,
			NoColors: false,
		}
	}
	var msg = ""
	switch eventType {
	case "push":
		msg = receive_push(event, cfg)
	case "commit_comment":
		msg = receive_commit_comment(event, cfg)
	case "pull_request":
		msg = receive_pull_request(event, cfg)
	case "pull_request_review_comment":
		msg = receive_pull_request_review_comment(event, cfg)
	case "issues": // random plural
		msg = receive_issues(event, cfg)
	case "issue_comment":
		msg = receive_issue_comment(event, cfg)
	case "gollum":
		msg = receive_gollum(event, cfg)
	default:
		//receive_unknown(eventType, event, cfg)
	}

	if cfg.NoColors {
		msg = colorRE.ReplaceAllString(msg, "")
	}

	return msg
}

func getDistinctCommits(event *GHEvent) []*GHCommit {
	var distinctCommits []*GHCommit
	for i := range event.Commits {
		commit := &event.Commits[i]
		if commit.Distinct && strings.TrimSpace(commit.Message) != "" {
			distinctCommits = append(distinctCommits, commit)
		}
	}
	return distinctCommits
}

func receive_push(event *GHEvent, cfg *EventFormatterOptions) string {
	if !cfg.branchNameMatches(event) {
		return ""
	}

	distinct_commits := getDistinctCommits(event)
	summary_message := irc_push_summary_message(event)
	summary_url := cfg.maybe_shorten(irc_push_summary_url(event))

	var messages []string
	messages = append(messages, fmt.Sprintf("%s: %s", summary_message, fmt_url(summary_url)))
	for i, commit := range distinct_commits {
		if i >= 3 {
			break
		}
		messages = append(messages, irc_format_commit_message(event, commit))
	}

	return strings.Join(messages, "\n")
}

func receive_commit_comment(event *GHEvent, cfg *EventFormatterOptions) string {
	summary_message := irc_commit_comment_summary_message(event)
	summary_url := cfg.maybe_shorten(irc_commit_comment_summary_url(event))
	return fmt.Sprintf("%s %s", summary_message, fmt_url(summary_url))
}

func receive_pull_request(event *GHEvent, cfg *EventFormatterOptions) string {
	action := event.Action
	if strings.Contains(action, "open") || strings.Contains(action, "close") {
		summary_message := irc_pull_request_summary_message(event)
		summary_url := cfg.maybe_shorten(irc_pull_request_summary_url(event))
		return fmt.Sprintf("%s %s", summary_message, fmt_url(summary_url))
	}
	return ""
}

func receive_pull_request_review_comment(event *GHEvent, cfg *EventFormatterOptions) string {
	summary_message := irc_pull_request_review_comment_summary_message(event)
	summary_url := cfg.maybe_shorten(irc_pull_request_review_comment_summary_url(event))
	return fmt.Sprintf("%s %s", summary_message, fmt_url(summary_url))
}

func receive_issues(event *GHEvent, cfg *EventFormatterOptions) string {
	action := event.Action
	if strings.Contains(action, "open") || strings.Contains(action, "close") {
		summary_message := irc_issue_summary_message(event)
		summary_url := cfg.maybe_shorten(irc_issue_summary_url(event))
		return fmt.Sprintf("%s %s", summary_message, fmt_url(summary_url))
	}
	return ""
}
func receive_issue_comment(event *GHEvent, cfg *EventFormatterOptions) string {
	action := event.Action
	if action == "edited" {
		// TODO: only ignore edits for a short window after the comment was created?
		// so if someone immediately fixes a typo we won't get a duplicate notification,
		// but if they go back a week later and change something we will
		return ""
	}
	summary_message := irc_issue_comment_summary_message(event)
	summary_url := cfg.maybe_shorten(irc_issue_comment_summary_url(event))
	return fmt.Sprintf("%s %s", summary_message, fmt_url(summary_url))
}

func receive_gollum(event *GHEvent, cfg *EventFormatterOptions) string {
	summary_message := irc_gollum_summary_message(event)
	summary_url := irc_gollum_summary_url(event) // not shortened
	return fmt.Sprintf("%s %s", summary_message, fmt_url(summary_url))
}

var colorRE = regexp.MustCompile(`\002|\017|\026|\037|\003\d{0,2}(?:,\d{1,2})?`)

/*
func irc_realname() string {
	repo_name := payload["repository"]["name"]
	repo_private := payload["repository"]["private"]
	if repo_private {
		return fmt.Sprintf("GitHub IRCBot - %s/%s", repo_owner, repo_name)
	}
	return fmt.Sprintf("GitHub IRCBot - %s", repo_owner)
}

func repo_owner() string {
	// for (what I presume to be) legacy reasonings, some events send owner login,
	// others send owner name. this method accounts for both cases.
	// sample: push event returns owner name, pull request event returns owner login
	if payload["repository"]["owner"]["name"] {
		return payload["repository"]["owner"]["name"]
	} else {
		return payload["repository"]["owner"]["login"]
	}
}
*/

func (data *EventFormatterOptions) maybe_shorten(summary_url string) string {
	if data.LongURL {
		return summary_url
	} else {
		return shorten_url(summary_url)
	}
}

/// IRC message formatting.  For reference:
/// \002 bold   \003 color   \017 reset  \026 italic/reverse  \037 underline
/// 0 white           1 black         2 dark blue         3 dark green
/// 4 dark red        5 brownish      6 dark purple       7 orange
/// 8 yellow          9 light green   10 dark teal        11 light teal
/// 12 light blue     13 light purple 14 dark gray        15 light gray

func fmt_url(s string) string    { return "\00302\037" + s + "\017" }
func fmt_repo(s string) string   { return "\00313" + s + "\017" }
func fmt_name(s string) string   { return "\00315" + s + "\017" }
func fmt_branch(s string) string { return "\00306" + s + "\017" }
func fmt_tag(s string) string    { return "\00306" + s + "\017" }
func fmt_hash(s string) string   { return "\00314" + s + "\017" }

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	} else {
		return plural
	}
}

func irc_push_summary_message(event *GHEvent) string {
	var b bytes.Buffer

	pusher_name := "somebody"
	if event.Pusher.Name != "" {
		pusher_name = event.Pusher.Name
	}

	repo_name := event.Repository.Name

	fmt.Fprintf(&b, "[%v] %v", fmt_repo(repo_name), fmt_name(pusher_name))

	distinct_commits := getDistinctCommits(event)

	tag := strings.HasPrefix(event.Ref, "refs/tags/")
	base_ref := event.BaseRef
	before_sha := event.Before[0:7]
	after_sha := event.After[0:7]
	tag_name := event.ref_name()
	branch_name := event.ref_name()
	base_ref_name := event.base_ref_name()

	if event.created() {
		if tag {
			fmt.Fprintf(&b, " tagged %s at", fmt_tag(tag_name))
			if base_ref != "" {
				fmt.Fprintf(&b, " %v", fmt_branch(base_ref_name))
			} else {
				fmt.Fprintf(&b, " %v", fmt_hash(after_sha))
			}
		} else {
			fmt.Fprintf(&b, " created %s", fmt_branch(branch_name))

			if base_ref != "" {
				fmt.Fprintf(&b, " from %s", fmt_branch(base_ref_name))
			} else if len(distinct_commits) == 0 {
				fmt.Fprintf(&b, " at %s", fmt_hash(after_sha))
			}

			num := len(distinct_commits)
			fmt.Fprintf(&b, " (+\002%d\017 new commit%s)", num, plural(num, "", "s"))
		}

	} else if event.deleted() {
		fmt.Fprintf(&b, " \00304deleted\017 %s at %s", fmt_branch(branch_name), fmt_hash(before_sha))

	} else if event.forced() {
		fmt.Fprintf(&b, " \00304force-pushed\017 %s from %s to %s", fmt_branch(branch_name), fmt_hash(before_sha), fmt_hash(after_sha))

	} else if len(event.Commits) > 0 && len(distinct_commits) == 0 {
		if base_ref != "" {
			fmt.Fprintf(&b, " merged %s into %s", fmt_branch(base_ref_name), fmt_branch(branch_name))
		} else {
			fmt.Fprintf(&b, " fast-forwarded %s from %s to %s", fmt_branch(branch_name), fmt_hash(before_sha), fmt_hash(after_sha))
		}

	} else {
		num := len(distinct_commits)
		fmt.Fprintf(&b, " pushed \002%d\017 new commit%s to %s", num, plural(num, "", "s"), fmt_branch(branch_name))
	}

	return b.String()
}

var allZeroRef = strings.Repeat("0", 40)

func (event *GHEvent) created() bool { return event.Created && event.Before == allZeroRef }
func (event *GHEvent) deleted() bool { return event.Deleted && event.After == allZeroRef }
func (event *GHEvent) forced() bool  { return event.Forced }

func (event *GHEvent) ref_name() string {
	if strings.HasPrefix(event.Ref, "refs/tags/") {
		return strings.TrimPrefix(event.Ref, "refs/tags/")
	}
	return strings.TrimPrefix(event.Ref, "refs/heads/")
}

func (event *GHEvent) base_ref_name() string {
	if strings.HasPrefix(event.BaseRef, "refs/tags/") {
		return strings.TrimPrefix(event.BaseRef, "refs/tags/")
	}
	return strings.TrimPrefix(event.BaseRef, "refs/heads/")
}

func firstLineOf(s string) string {
	newline := strings.Index(s, "\n")
	if newline >= 0 {
		s = s[:newline] + "..."
	}
	return s
}

func irc_format_commit_message(event *GHEvent, commit *GHCommit) string {
	short := firstLineOf(commit.Message)

	repo_name := event.Repository.Name
	branch_name := event.ref_name()

	author := commit.Author.Name
	sha1 := commit.ID

	return fmt.Sprintf("%v/%v %v %v: %s",
		fmt_repo(repo_name), fmt_branch(branch_name), fmt_hash(sha1[0:7]), fmt_name(author), short)
}

func irc_issue_summary_message(event *GHEvent) string {
	repo := &event.Repository
	sender := &event.Sender
	issue := event.Issue
	return fmt.Sprintf("[%s] %s %s issue #%d: %s", fmt_repo(repo.Name), fmt_name(sender.Login), event.Action, issue.Number, issue.Title)
}

func irc_issue_comment_summary_message(event *GHEvent) string {
	repo := &event.Repository
	sender := &event.Sender
	issue := event.Issue
	short := firstLineOf(event.Comment.Body)
	return fmt.Sprintf("[%s] %s commented on issue #%d: %s", fmt_repo(repo.Name), fmt_name(sender.Login), issue.Number, short)
}

func irc_commit_comment_summary_message(event *GHEvent) string {
	repo := &event.Repository
	sender := &event.Sender
	comment := event.Comment
	short := firstLineOf(comment.Body)
	sha1 := comment.CommitID
	return fmt.Sprintf("[%s] %s commented on commit %s: %s", fmt_repo(repo.Name), fmt_name(sender.Login), fmt_hash(sha1[0:7]), short)
}

func irc_pull_request_summary_message(event *GHEvent) string {
	repo := &event.Repository
	sender := &event.Sender
	pull := event.PullRequest
	base_ref := event.PullRequest.Base.Ref
	head_ref := event.PullRequest.Head.Ref
	head_label := head_ref
	if head_ref == base_ref {
		head_label = event.PullRequest.Head.Label
	}

	return fmt.Sprintf("[%v] %v %v pull request #%v: %v (%v...%v)",
		fmt_repo(repo.Name), fmt_name(sender.Login), event.Action, pull.Number, pull.Title, fmt_branch(base_ref), fmt_branch(head_label))
}

func irc_pull_request_review_comment_summary_message(event *GHEvent) string {
	repo := &event.Repository
	sender := &event.Sender
	comment := event.Comment
	short, _ := partition(comment.Body, "\r\n")
	if short != comment.Body {
		short += "..."
	}
	sha1 := comment.CommitID
	//pull_request_number := comment.pull_request_url =~ /\/(\d+)$/
	pull_request_number := event.PullRequest.Number

	return fmt.Sprintf("[%v] %v commented on pull request #%v %v: %v",
		fmt_repo(repo.Name), fmt_name(sender.Login), pull_request_number, fmt_hash(sha1[0:7]), short)
}

func irc_gollum_summary_message(event *GHEvent) string {
	repo := &event.Repository
	sender := &event.Sender
	if len(event.Pages) == 1 {
		summary := event.Pages[0].Summary
		msg := fmt.Sprintf("[%s] %s %s wiki page %s",
			repo.Name,
			sender.Login,
			event.Pages[0].Action,
			event.Pages[0].Title)
		if summary != "" {
			msg += ": " + summary
		}
		return msg
	} else {
		var counts = make(map[string]int)
		for i := range event.Pages {
			counts[event.Pages[i].Action] += 1
		}

		var actions []string
		for action, count := range counts {
			actions = append(actions, fmt.Sprintf("%s %d", action, count))
		}
		sort.Strings(actions)

		return fmt.Sprintf("[%s] %s %s wiki pages",
			repo.Name,
			sender.Login,
			toSentence(actions),
		)
	}
}

func toSentence(a []string) string {
	switch len(a) {
	case 0:
		return ""
	case 1:
		return a[0]
	case 2:
		return a[0] + " and " + a[1]
	default:
		return strings.Join(a[:len(a)-1], ", ") + ", and " + a[len(a)-1]
	}
}

func irc_push_summary_url(event *GHEvent) string {
	distinct_commits := getDistinctCommits(event)
	repo_url := event.Repository.URL
	before_sha := event.Before[0:7]
	if event.created() {
		if len(distinct_commits) == 0 {
			return repo_url + "/commits/" + event.ref_name()
		} else {
			return event.Compare
		}
	} else if event.deleted() {
		return repo_url + "/commit/" + before_sha
	} else if event.forced() {
		return event.Compare
	} else if len(distinct_commits) == 1 {
		return distinct_commits[0].URL
	} else {
		return event.Compare
	}
}

func irc_commit_comment_summary_url(event *GHEvent) string {
	return event.Comment.HtmlUrl
}

func irc_pull_request_summary_url(event *GHEvent) string {
	return event.PullRequest.HtmlUrl
}
func irc_pull_request_review_comment_summary_url(event *GHEvent) string {
	return event.PullRequest.HtmlUrl
}

func irc_issue_summary_url(event *GHEvent) string {
	return event.Issue.HtmlUrl
}

func irc_issue_comment_summary_url(event *GHEvent) string {
	return event.Issue.HtmlUrl
}

func irc_gollum_summary_url(event *GHEvent) string {
	if len(event.Pages) == 1 {
		return event.Pages[0].HtmlUrl
	} else {
		return event.Repository.URL + "/wiki"
	}
}

func (data *EventFormatterOptions) branchNameMatches(event *GHEvent) bool {
	if strings.TrimSpace(data.Branches) == "" {
		return true
	}
	branch_name := event.ref_name()
	branches := strings.Split(data.Branches, ",")
	for _, b := range branches {
		if branch_name == b {
			return true
		}
	}
	return false
}

// Shortens the given URL with git.io.
//
// url - String URL to be shortened.
//
// Returns the String URL response from git.io.
func shorten_url(url string) string {
	c := &http.Client{
		Timeout: 15 * time.Second,
	}
	resp, err := c.PostForm("https://git.io", urlpkg.Values{"url": []string{url}})
	if err == nil && resp.StatusCode == 201 {
		return resp.Header.Get("Location")
	} else {
		return url
	}
}

func partition(s, sep string) (head, tail string) {
	i := strings.Index(s, sep)
	if i < 0 {
		return s, ""
	}
	return s[0:i], s[i+len(sep):]
}
