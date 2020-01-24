package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
)

type Service struct {
	server, port, room, nick, branches                     string
	room                                                   []string
	password, nickserv_password                            string
	ssl, message_without_join, no_colors, long_url, notice bool
	//white_list :server, :port, :room, :nick

	//default_events :push, :pull_request
}

func receive_push() {
	if !branchNameMatches() {
		return
	}

	var messages []string
	messages = append(messages, "#{irc_push_summary_message}: #{fmt_url url}")
	for i, commit := range distinct_commits {
		if i >= 3 {
			break
		}
		messages = append(messages, irc_format_commit_message(commit))
	}
	send_messages(messages...)
}

func receive_commit_comment() {
	send_messages("#{irc_commit_comment_summary_message} #{fmt_url url}")
}

func receive_pull_request() {
	if strings.Contains(action, "open") || strings.Contains(action, "close") {
		send_messages("#{irc_pull_request_summary_message} #{fmt_url url}")
	}
}

func receive_pull_request_review_comment() {
	send_messages("#{irc_pull_request_review_comment_summary_message} #{fmt_url url}")
}

func receive_issues() {
	if strings.Contains(action, "open") || strings.Contains(action, "close") {
		send_messages("#{irc_issue_summary_message} #{fmt_url url}")
	}
}
func receive_issue_comment() {
	send_messages("#{irc_issue_comment_summary_message} #{fmt_url url}")
}

func receive_gollum() {
	send_messages("#{irc_gollum_summary_message} #{fmt_url summary_url}")
}

func send_messages(messages ...string) error {
	if data.no_colors {
		for i := range messages {
			messages[i].gsub(`/\002|\017|\026|\037|\003\d{0,2}(?:,\d{1,2})?/`, "")
		}
	}

	rooms := data.Rooms
	if len(rooms) == 0 {
		return fmt.Errorf("No rooms: %#v", rooms)
	}

	//XXX rooms   = rooms.gsub(",", " ").split(" ").map{|room| room[0].chr == "#" ? room : "##{room}"}
	botname := data.Nick[0:17]
	if data.Nick == "" {
		botname = fmt.Sprint("GitHub", rand.Intn(200))
	}
	command := "NOTICE"
	if data.Notice {
		command = "PRIVMSG"
	}

	if data.Password != "" {
		irc_password("PASS", data.Password)
	}
	irc_printf("NICK %s", botname)
	irc_printf("USER %s 8 * :%s", botname, irc_realname)

	for {
		line := irc_gets()
		if regexp.MatchString(` 00[1-4] `+regexp.QuoteMeta(botname)+` `, line) {
			break
		} else if regexp.MatchString(`^PING\s*:\s*(.*)$`, line) {
			re := regexp.MustCompile(`^PING\s*:\s*(.*)$`)
			submatches := re.FindStringSubmatchIndex(line)
			pong := re.Expand(nil, "$1", line, submatches)
			irc_printf("PONG %s", pong)
		}
	}

	nickserv_password := data.NickservPassword
	if nickserv_password != "" {
		irc_password("PRIVMSG NICKSERV :IDENTIFY", nickserv_password)
		for {
			line := irc_gets()
			if regexp.MatchString(`(?i)^:NickServ/`, line) {
				// NickServ responded somehow.
				break
			} else if regexp.MatchString(`^PING\s*:\s*(.*)$`, line) {
				irc_puts("PONG #{$1}")
			}
		}
	}

	without_join := data.message_without_join
	for _, room := range rooms {
		room, pass = strings.Split(room, "::")
		if !without_join {
			irc_printf("JOIN %s %s", room, pass)
		}

		for _, message := range messages {
			irc_printf("%s %s :%s", command, room, message)
		}

		if !without_join {
			irc_printf("PART %s", room)
		}
	}

	irc_puts("QUIT")
	for !irc_eof() {
		irc_gets()
	}
	/*
	  rescue SocketError => boom
	    if boom.to_s =~ /getaddrinfo: Name or service not known/ {
	      raise_config_error("Invalid host")
	    } else if boom.to_s =~ /getaddrinfo: Servname not supported for ai_socktype/ {
	      raise_config_error("Invalid port")
	    } else {
	      raise
	    }
	  rescue Errno::ECONNREFUSED, Errno::EHOSTUNREACH
	    raise_config_error("Invalid host")
	  rescue OpenSSL::SSL::SSLError
	    raise_config_error("Host does not support SSL")
	  ensure
	    emit_debug_log
	*/
}

func irc_gets() {
	response := readable_irc.ReadString('\n')
	if response != "" {
		debug_incoming(response)
	}
	response
}

func irc_eof() {
	readable_irc.eof
}

func irc_password(command, password string) {
	real_command := fmt.Sprintf("%s %s", command, password)
	debug_command = fmt.Sprintf("%s %s", command, strings.Repeat("*", len(password)))
	irc_puts_debug(real_command, debug_command)
}

func irc_puts_debug(command, debug_command) {
	debug_outgoing(debug_command)
	writable_irc.puts(command)
}
func irc_puts(command) {
	debug_outgoing(command)
	writable_irc.puts(command)
}

func irc_realname() {
	repo_name := payload["repository"]["name"]
	repo_private := payload["repository"]["private"]
	if repo_private {
		return "GitHub IRCBot - #{repo_owner}/#{repo_name}"
	}
	return "GitHub IRCBot - #{repo_owner}"
}

func repo_owner() {
	// for (what I presume to be) legacy reasonings, some events send owner login,
	// others send owner name. this method accounts for both cases.
	// sample: push event returns owner name, pull request event returns owner login
	if payload["repository"]["owner"]["name"] {
		return payload["repository"]["owner"]["name"]
	} else {
		return payload["repository"]["owner"]["login"]
	}
}

func debug_outgoing(command) {
	irc_debug_log << ">> #{command.strip}"
}

func debug_incoming(command) {
	irc_debug_log << "=> #{command.strip}"
}

func irc_debug_log() {
	//@irc_debug_log ||= []
}

func emit_debug_log() {
	if len(irc_debug_log) > 0 {
		receive_remote_call("IRC Log:\n#{irc_debug_log.join('\n')}")
	}
}

func irc() {
	/*
	   @irc ||= begin
	     socket = TCPSocket.open(data["server"], port)
	     if (use_ssl) {
	     socket = new_ssl_wrapper(socket)
	     }
	     socket
	   }
	*/
}

//alias readable_irc irc
//alias writable_irc irc

func new_ssl_wrapper(socket) {
	/*
	   ssl_context = OpenSSL::SSL::SSLContext.new
	   ssl_context.verify_mode = OpenSSL::SSL::VERIFY_NONE
	   ssl_socket = OpenSSL::SSL::SSLSocket.new(socket, ssl_context)
	   ssl_socket.sync_close = true
	   ssl_socket.connect
	   ssl_socket
	*/
}

func use_ssl() {
	return data.ssl
}

func default_port() {
	if use_ssl() {
		return 6697
	} else {
		return 6667
	}
}

func port() {
	port, err := strconv.ParseInt(data.Port, 10)
	if err == nil && port > 0 {
		return port
	}
	return default_port()
}

func url() {
	if config_boolean_true("long_url") {
		return summary_url
	} else {
		shorten_url(summary_url)
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

func irc_push_summary_message() {
	var b bytes.Buffer
	fmt.Printf(&b, "[%v] %v", fmt_repo(repo_name), fmt_name(pusher_name))

	if created {
		if tag {
			fmt.Fprintf(&b, " tagged %s at", fmt_tag(tag_name))
			if base_ref {
				fmt.Fprintf(&b, " %v", fmt_branch(base_ref_name))
			} else {
				fmt.Fprintf(&b, " %v", fmt_hash(after_sha))
			}
		} else {
			fmt.Fprintf(&b, " created %s", fmt_branch(branch_name))

			if base_ref {
				fmt.Fprintf(&b, " from %s", fmt_branch(base_ref_name))
			} else if distinct_commits.empty {
				fmt.Fprintf(&b, " at %s", fmt_hash(after_sha))
			}

			num := len(distinct_commits)
			fmt.Fprintf(&b, " (+\002%d\017 new commit%s)", num, plural(num, "", "s"))
		}

	} else if deleted {
		fmt.Fprintf(&b, " \00304deleted\017 #{fmt_branch branch_name} at #{fmt_hash before_sha}")

	} else if forced {
		fmt.Fprintf(&b, " \00304force-pushed\017 #{fmt_branch branch_name} from #{fmt_hash before_sha} to #{fmt_hash after_sha}")

	} else if commits.any && distinct_commits.empty {
		if base_ref {
			fmt.Fprintf(&b, " merged #{fmt_branch base_ref_name} into #{fmt_branch branch_name}")
		} else {
			fmt.Fprintf(&b, " fast-forwarded #{fmt_branch branch_name} from #{fmt_hash before_sha} to #{fmt_hash after_sha}")
		}

	} else {
		num = distinct_commits.size
		fmt.Fprintf(&b, " pushed \002%d\017 new commit%s to %s", num, plural(num, "", "s"), fmt_branch(branch_name))
	}

	return b.String()
}

func firstLineOf(s string) string {
	newline := strings.Index(s, "\n")
	if newline >= 0 {
		s = s[:newline] + "..."
	}
	return s
}

func irc_format_commit_message(commit) {
	short := firstLineOf(commit["message"])

	author := commit["author"]["name"]
	sha1 := commit["id"]
	files := commit["modified"]

	return fmt.Sprintf("%v/%v %v %v: #{short}",
		fmt_repo(repo_name), fmt_branch(branch_name), fmt_hash(sha1[0:7]), fmt_name(author), short)
}

func irc_issue_summary_message() {
	return "[#{fmt_repo repo.name}] #{fmt_name sender.login} #{action} issue \\##{issue.number}: #{issue.title}"
}

func irc_issue_comment_summary_message() {
	short = firstLineOf(comment.body)
	return "[#{fmt_repo repo.name}] #{fmt_name sender.login} commented on issue \\##{issue.number}: #{short}"
}

func irc_commit_comment_summary_message() string {
	short = firstLineOf(comment.body)
	sha1 = comment.commit_id
	return "[#{fmt_repo repo.name}] #{fmt_name sender.login} commented on commit #{fmt_hash sha1[0:7]}: #{short}"
}

func irc_pull_request_summary_message() string {
	base_ref := pull.base.label.split(":").last
	head_ref := pull.head.label.split(":").last
	head_label := head_ref
	if head_ref == base_ref {
		pull.head.label
	}

	return fmt.Sprintf("[%v] %v %v pull request #%v: %v (%v...%v)",
		fmt_repo(repo.name), fmt_name(sender.login), action, pull.number, pull.title, fmt_branch(base_ref), fmt_branch(head_ref))
}

func irc_pull_request_review_comment_summary_message() string {
	short = comment.body.split("\r\n", 2).first.to_s
	if short != comment.body {
		short += "..."
	}
	sha1 = comment.commit_id
	return fmt.Sprintf("[%v] %v commented on pull request #%v %v: %v",
		fmt_repo(repo.name), fmt_name(sender.login), pull_request_number, fmt_hash(sha1[0:7]))
}

func irc_gollum_summary_message() {
	summary_message
}

func branchNameMatches() {
	if strings.TrimSpace(data.Branches) == "" {
		return true
	}
	branches := strings.Split(data.Branches, ",")
	for _, b := range branches {
		if branch_name == b {
			return true
		}
	}
}
