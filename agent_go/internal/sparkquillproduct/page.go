package sparkquillproduct

// The activity page finisher.
//
// Quill writes an activity page however it likes: its own title, styles,
// animations, pictures, demos, a story. This file does not impose a look. It
// adds only what the child tutor relies on and removes only what would break
// for a child:
//
//   - stable ids on sections (s1, s2…), questions (q1, q2…) and figures
//     (fig1…), so the tutor can scroll her to the right place and record what
//     she answered beside the question;
//   - an answer space inside every question, replaced by the tutor's note
//     once she has answered;
//   - the SQ choice/answer bridge and print hook, so page buttons send the
//     reader's turn and the app's print icon works;
//   - a section map (role and question ids per section) for activity.json.
//
// Removed: <details> click-to-reveal (its content is left in the open; a hidden answer is
// invisible to the tutor), links, and anything loaded from the internet
// (blank offline and in print).

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Section roles. The tutor's default behaviour follows the section the child
// is in.
const (
	RoleLearn    = "learn"    // explain freely, confirm answers
	RolePractice = "practice" // hints first, then the answer
	RoleCheck    = "check"    // a real test: hints only, no right/wrong until done
	RoleExplore  = "explore"  // open-ended, follow her curiosity
)

var knownRoles = map[string]bool{RoleLearn: true, RolePractice: true, RoleCheck: true, RoleExplore: true}

// SectionInfo is one entry of the section map written into activity.json.
type SectionInfo struct {
	ID        string   `json:"id"`
	Title     string   `json:"title,omitempty"`
	Role      string   `json:"role"`
	Questions []string `json:"questions,omitempty"`
}

// PageMeta is what the finisher knows about the activity; it only supplies a
// <title> when the page has no <h1> or <title> of its own.
type PageMeta struct {
	Title string
}

// RenderReport tells the tool (and through it, Quill) what the page came out
// as and what had to be removed.
type RenderReport struct {
	Title     string        `json:"title"`
	Sections  []SectionInfo `json:"sections"`
	Questions int           `json:"questions"`
	Marks     int           `json:"marks"`
	Dropped   []string      `json:"dropped,omitempty"`
	Warnings  []string      `json:"warnings,omitempty"`
}

var (
	// Removed with their content.
	dropElements = map[atom.Atom]bool{
		atom.Iframe: true, atom.Link: true, atom.Object: true, atom.Embed: true, atom.Base: true,
	}
	// Unwrapped: the element goes, its children stay.
	unwrapElements = map[atom.Atom]bool{
		atom.Details: true, atom.Summary: true, atom.A: true, atom.Font: true, atom.Center: true, atom.Form: true,
	}
	urlScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

type renderer struct {
	report  RenderReport
	title   string
	head    []string // Quill's own <style> blocks, kept in the head
	body    bytes.Buffer
	current *SectionInfo
	nextS   int
	nextQ   int
	nextFig int
}

// RenderActivityPage finishes a page Quill wrote, whether it is a fragment or
// a whole document.
func RenderActivityPage(source string, meta PageMeta) (string, RenderReport, error) {
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil {
		return "", RenderReport{}, fmt.Errorf("parse activity page: %w", err)
	}
	r := &renderer{title: strings.TrimSpace(meta.Title)}
	var headNode, bodyNode *xhtml.Node
	var find func(*xhtml.Node)
	find = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.DataAtom {
			case atom.Head:
				headNode = n
			case atom.Body:
				bodyNode = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if headNode != nil {
		for c := headNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Style:
				var b bytes.Buffer
				xhtml.Render(&b, c) //nolint:errcheck
				r.head = append(r.head, b.String())
			case atom.Title:
				if r.title == "" {
					r.title = strings.TrimSpace(textOf(c))
				}
			case atom.Link, atom.Script:
				r.report.Dropped = append(r.report.Dropped, "<"+c.Data+"> in <head> (nothing may be loaded from elsewhere; put scripts in the body)")
			}
		}
	}
	if bodyNode != nil {
		for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
			r.node(c, &r.body)
		}
	}
	if strings.TrimSpace(r.body.String()) == "" {
		return "", r.report, fmt.Errorf("the page has no content")
	}
	if r.title == "" {
		r.title = "Activity"
		r.report.Warnings = append(r.report.Warnings, "no title: add an <h1> (or <title>)")
	}
	r.report.Title = r.title
	return r.page(), r.report, nil
}

// node writes one finished node into out.
func (r *renderer) node(n *xhtml.Node, out *bytes.Buffer) {
	switch n.Type {
	case xhtml.TextNode:
		out.WriteString(html.EscapeString(n.Data))
		return
	case xhtml.ElementNode:
	default:
		return
	}
	if n.DataAtom == atom.Style {
		var b bytes.Buffer
		xhtml.Render(&b, n) //nolint:errcheck
		r.head = append(r.head, b.String())
		return
	}
	if dropElements[n.DataAtom] {
		r.report.Dropped = append(r.report.Dropped, "<"+n.Data+">")
		return
	}
	if unwrapElements[n.DataAtom] {
		if n.DataAtom == atom.Details || n.DataAtom == atom.Summary {
			r.report.Dropped = append(r.report.Dropped, "<"+n.Data+"> (click-to-reveal; its content is now in the open)")
		}
		r.children(n, out)
		return
	}
	if n.DataAtom == atom.Script || n.DataAtom == atom.Svg || n.Data == "svg" || n.Data == "math" {
		if src := strings.TrimSpace(attr(n, "src")); src != "" && (urlScheme.MatchString(src) || strings.HasPrefix(src, "//")) {
			r.report.Dropped = append(r.report.Dropped, fmt.Sprintf("<script src=%q> (nothing may be loaded from the internet; JSXGraph is supplied by the app)", src))
			return
		}
		xhtml.Render(out, n) //nolint:errcheck
		return
	}
	if n.DataAtom == atom.H1 && r.title == "" {
		r.title = strings.TrimSpace(textOf(n))
	}
	switch {
	case n.DataAtom == atom.Section:
		r.section(n, out)
		return
	case hasClass(n, "q"):
		r.question(n, out)
		return
	case n.DataAtom == atom.Figure:
		r.nextFig++
		r.open(n, out, fmt.Sprintf("fig%d", r.nextFig))
		r.children(n, out)
		out.WriteString("</figure>")
		return
	}
	r.open(n, out, "")
	if isVoid(n.DataAtom) {
		return
	}
	r.children(n, out)
	out.WriteString("</" + n.Data + ">")
}

// open writes the opening tag with Quill's own attributes, minus anything
// that reaches out to the internet, and with the id the tutor relies on.
func (r *renderer) open(n *xhtml.Node, out *bytes.Buffer, id string) {
	out.WriteString("<" + n.Data)
	if id != "" {
		fmt.Fprintf(out, ` id="%s"`, id)
	}
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		val := strings.TrimSpace(a.Val)
		if key == "id" && id != "" {
			continue
		}
		if (key == "src" || key == "href" || key == "poster" || key == "srcset") && (urlScheme.MatchString(val) || strings.HasPrefix(val, "//")) {
			r.report.Dropped = append(r.report.Dropped, fmt.Sprintf("<%s %s=%q> (remote; only files beside the page work offline and in print)", n.Data, key, val))
			continue
		}
		fmt.Fprintf(out, ` %s="%s"`, key, html.EscapeString(a.Val))
	}
	out.WriteString(">")
}

func (r *renderer) children(n *xhtml.Node, out *bytes.Buffer) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.node(c, out)
	}
}

func (r *renderer) section(n *xhtml.Node, out *bytes.Buffer) {
	role := strings.ToLower(strings.TrimSpace(attr(n, "data-role")))
	if !knownRoles[role] {
		if role != "" {
			r.report.Warnings = append(r.report.Warnings, fmt.Sprintf("unknown section role %q treated as learn", role))
		}
		role = RoleLearn
	}
	r.nextS++
	info := SectionInfo{ID: fmt.Sprintf("s%d", r.nextS), Role: role}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && (c.DataAtom == atom.H1 || c.DataAtom == atom.H2 || c.DataAtom == atom.H3) {
			info.Title = strings.TrimSpace(textOf(c))
			break
		}
	}
	r.report.Sections = append(r.report.Sections, info)
	r.current = &r.report.Sections[len(r.report.Sections)-1]
	out.WriteString("<section")
	fmt.Fprintf(out, ` id="%s" data-role="%s"`, info.ID, role)
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if key == "id" || key == "data-role" {
			continue
		}
		fmt.Fprintf(out, ` %s="%s"`, key, html.EscapeString(a.Val))
	}
	out.WriteString(">")
	r.children(n, out)
	out.WriteString("</section>")
	r.current = nil
}

// question numbers the question across the page and leaves the answer space
// the tutor replaces when she answers. Marks stay as Quill's data attribute
// so it can show them however it likes.
func (r *renderer) question(n *xhtml.Node, out *bytes.Buffer) {
	r.nextQ++
	id := fmt.Sprintf("q%d", r.nextQ)
	if r.current != nil {
		r.current.Questions = append(r.current.Questions, id)
	}
	r.report.Questions++
	marks, _ := strconv.Atoi(strings.TrimSpace(attr(n, "data-marks")))
	if marks > 0 {
		r.report.Marks += marks
	}
	if strings.TrimSpace(textOf(n)) == "" {
		r.report.Warnings = append(r.report.Warnings, id+" has no text")
	}
	r.open(n, out, id)
	r.children(n, out)
	out.WriteString(`<div class="answer-space"></div></` + n.Data + ">")
}

func (r *renderer) page() string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n<style>%s</style>\n", html.EscapeString(r.title), functionalCSS)
	fmt.Fprintf(&b, "<script>%s</script>\n", activityScript)
	for _, h := range r.head {
		b.WriteString(h)
		b.WriteString("\n")
	}
	b.WriteString("</head>\n<body>\n")
	b.Write(r.body.Bytes())
	b.WriteString("\n</body>\n</html>\n")
	return b.String()
}

func hasClass(n *xhtml.Node, class string) bool {
	for _, tok := range strings.Fields(attr(n, "class")) {
		if strings.EqualFold(tok, class) {
			return true
		}
	}
	return false
}

func isVoid(a atom.Atom) bool {
	switch a {
	case atom.Br, atom.Hr, atom.Img, atom.Input, atom.Source, atom.Track, atom.Wbr, atom.Area, atom.Col, atom.Meta:
		return true
	}
	return false
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.TextNode {
			b.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return b.String()
}

// functionalCSS is the only style the finisher adds: the answer space and the
// tutor's note look like themselves whatever Quill designed, and a tapped
// choice button visibly disables. Nothing here is a look.
const functionalCSS = `
.answer-space{min-height:72px;margin-top:8px;border:1px dashed rgba(0,0,0,.25);border-radius:8px}
.answered-note{opacity:.8;font-size:.9em;margin:8px 0 0;padding:6px 10px;border-left:3px solid rgba(0,0,0,.25)}
button:disabled{opacity:.55;cursor:default}
@media print{.answer-space{min-height:110px}}
`

// activityScript is the print hook and the SQ button bridge. A choice or
// answer sends its value as the reader's turn and disables itself at once so
// a slow reply cannot be tapped twice. data-choose buttons are wired on load.
const activityScript = `
window.addEventListener('message',function(e){if(e&&e.data&&e.data.__sq===1&&e.data.op==='print')window.print()});
var gameLoads={},nextGameLoad=0;
window.addEventListener('message',function(e){var m=e&&e.data;if(!m||m.__sq!==1||m.op!=='game-loaded'||!gameLoads[m.id])return;var done=gameLoads[m.id];delete gameLoads[m.id];done(m.data==null?null:m.data)});
window.SQ={
  choose:function(text,el){if(el&&el.disabled)return;if(el)el.disabled=true;parent.postMessage({__sq:1,op:'choose',text:text},'*')},
  answer:function(qid,value,el){if(el&&el.disabled)return;if(String(value==null?'':value).trim()==='')return;if(el)el.disabled=true;parent.postMessage({__sq:1,op:'answer',qid:qid,value:value},'*')},
  startTimers:function(timers){parent.postMessage({__sq:1,op:'timer-config',timers:timers},'*')},
  cancelTimer:function(qid){parent.postMessage({__sq:1,op:'timer-cancel',qid:qid},'*')},
  saveGame:function(key,data){parent.postMessage({__sq:1,op:'game-save',key:key,data:data},'*')},
  loadGame:function(key,done){if(typeof done!=='function')return;var id='game-'+(++nextGameLoad);gameLoads[id]=done;parent.postMessage({__sq:1,op:'game-load',key:key,id:id},'*')}
};
document.addEventListener('click',function(e){var b=e.target&&e.target.closest&&e.target.closest('button[data-choose]');if(b)SQ.choose(b.getAttribute('data-choose'),b)});
`
