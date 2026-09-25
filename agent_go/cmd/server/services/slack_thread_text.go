package services

import (
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slackMessageText returns the readable content of a Slack message for the
// agent's thread context. App posts (Sentry, GitHub, PagerDuty, ...) usually
// leave `text` empty or put only a short fallback there and carry the real
// content in attachments or Block Kit blocks, so those are flattened too.
// Blocks are read only when `text` is empty: for ordinary user messages the
// text field already is the rendering of the rich_text blocks.
func slackMessageText(msg slack.Message) string {
	var parts []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		parts = append(parts, s)
	}

	add(msg.Text)
	if strings.TrimSpace(msg.Text) == "" {
		for _, line := range slackBlocksText(msg.Blocks) {
			add(line)
		}
	}
	for _, att := range msg.Attachments {
		before := len(parts)
		add(att.Pretext)
		if att.AuthorName != "" {
			add(att.AuthorName)
		}
		title := strings.TrimSpace(att.Title)
		if title != "" && att.TitleLink != "" {
			title += " (" + att.TitleLink + ")"
		}
		add(title)
		add(att.Text)
		for _, field := range att.Fields {
			name, value := strings.TrimSpace(field.Title), strings.TrimSpace(field.Value)
			switch {
			case name != "" && value != "":
				add(name + ": " + value)
			default:
				add(name + value)
			}
		}
		for _, line := range slackBlocksText(att.Blocks) {
			add(line)
		}
		add(att.Footer)
		if len(parts) == before {
			// Nothing structured: the fallback is the only readable content.
			add(att.Fallback)
		}
	}
	return strings.Join(parts, "\n")
}

func slackBlocksText(blocks slack.Blocks) []string {
	var out []string
	textObj := func(t *slack.TextBlockObject) {
		if t != nil && strings.TrimSpace(t.Text) != "" {
			out = append(out, t.Text)
		}
	}
	for _, block := range blocks.BlockSet {
		switch b := block.(type) {
		case *slack.SectionBlock:
			textObj(b.Text)
			for _, f := range b.Fields {
				textObj(f)
			}
		case *slack.HeaderBlock:
			textObj(b.Text)
		case *slack.MarkdownBlock:
			if strings.TrimSpace(b.Text) != "" {
				out = append(out, b.Text)
			}
		case *slack.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if t, ok := el.(*slack.TextBlockObject); ok {
					textObj(t)
				}
			}
		case *slack.RichTextBlock:
			for _, el := range b.Elements {
				if s := strings.TrimSpace(slackRichTextElementText(el)); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

func slackRichTextElementText(el slack.RichTextElement) string {
	switch e := el.(type) {
	case *slack.RichTextSection:
		return slackRichTextSectionText(e.Elements)
	case *slack.RichTextQuote:
		return slackRichTextSectionText(e.Elements)
	case *slack.RichTextPreformatted:
		return slackRichTextSectionText(e.Elements)
	case *slack.RichTextList:
		var lines []string
		for _, item := range e.Elements {
			if s := strings.TrimSpace(slackRichTextElementText(item)); s != "" {
				lines = append(lines, "- "+s)
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func slackRichTextSectionText(elements []slack.RichTextSectionElement) string {
	var b strings.Builder
	for _, el := range elements {
		switch e := el.(type) {
		case *slack.RichTextSectionTextElement:
			b.WriteString(e.Text)
		case *slack.RichTextSectionLinkElement:
			if e.Text != "" && e.Text != e.URL {
				b.WriteString(e.Text + " (" + e.URL + ")")
			} else {
				b.WriteString(e.URL)
			}
		case *slack.RichTextSectionUserElement:
			b.WriteString("<@" + e.UserID + ">")
		case *slack.RichTextSectionChannelElement:
			b.WriteString("<#" + e.ChannelID + ">")
		case *slack.RichTextSectionEmojiElement:
			b.WriteString(":" + e.Name + ":")
		}
	}
	return b.String()
}

// slackMessageAuthorName names an app post by its app/bot name so the agent
// can tell a Sentry alert from a colleague. Humans keep their user id.
func slackMessageAuthorName(msg slack.Message) string {
	if msg.BotID == "" {
		return msg.User
	}
	if msg.BotProfile != nil && strings.TrimSpace(msg.BotProfile.Name) != "" {
		return strings.TrimSpace(msg.BotProfile.Name)
	}
	if strings.TrimSpace(msg.Username) != "" {
		return strings.TrimSpace(msg.Username)
	}
	return msg.BotID
}

// parseSlackTS converts a Slack message ts ("1710000000.000100") to a time
// with its microseconds kept, so messages within the same second still order
// correctly against the per-thread "already given" watermark.
func parseSlackTS(ts string) (time.Time, bool) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, false
	}
	secPart, fracPart, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(secPart, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	var micros int64
	if fracPart != "" {
		if len(fracPart) > 6 {
			fracPart = fracPart[:6]
		}
		for len(fracPart) < 6 {
			fracPart += "0"
		}
		micros, err = strconv.ParseInt(fracPart, 10, 64)
		if err != nil {
			return time.Time{}, false
		}
	}
	return time.Unix(sec, micros*1000), true
}
