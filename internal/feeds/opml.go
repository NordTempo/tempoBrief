package feeds

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/NordTempo/tempoBrief/internal/store"
)

type opml struct {
	XMLName xml.Name  `xml:"opml"`
	Version string    `xml:"version,attr"`
	Title   string    `xml:"head>title"`
	Body    []outline `xml:"body>outline"`
}

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Children []outline `xml:"outline"`
}

// ParseOPML reads feeds from an OPML export. A feed's topic is the title of the
// folder it sits in (the nearest one); feeds outside folders get no topic.
func ParseOPML(r io.Reader) ([]store.NewFeed, error) {
	var doc opml
	if err := xml.NewDecoder(io.LimitReader(r, maxBody)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("not an OPML file: %w", err)
	}
	var out []store.NewFeed
	var walk func(list []outline, topic string)
	walk = func(list []outline, topic string) {
		for _, o := range list {
			name := strings.TrimSpace(o.Title)
			if name == "" {
				name = strings.TrimSpace(o.Text)
			}
			if o.XMLURL != "" {
				out = append(out, store.NewFeed{URL: o.XMLURL, Title: name, Topic: topic})
			}
			if len(o.Children) > 0 {
				walk(o.Children, name)
			}
		}
	}
	walk(doc.Body, "")
	return out, nil
}

// WriteOPML exports feeds grouped into one folder per topic.
func WriteOPML(w io.Writer, feeds []store.Feed) error {
	doc := opml{Version: "2.0", Title: "tempoBrief feeds"}
	folders := map[string]int{}
	for _, f := range feeds {
		i, ok := folders[f.Topic]
		if !ok {
			i = len(doc.Body)
			folders[f.Topic] = i
			doc.Body = append(doc.Body, outline{Text: f.Topic, Title: f.Topic})
		}
		doc.Body[i].Children = append(doc.Body[i].Children, outline{
			Text: f.Title, Title: f.Title, Type: "rss", XMLURL: f.URL, HTMLURL: f.SiteURL,
		})
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	return enc.Encode(doc)
}
