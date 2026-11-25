package deliver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

// emailHTML is deliberately plain: inline styles only, since mail clients
// strip everything else. Dark text on white, because most clients force it.
var emailHTML = template.Must(template.New("email").Funcs(template.FuncMap{
	"sources": sourcesLabel, "score": scoreLabel,
}).Parse(`<!doctype html><html><body style="margin:0;padding:24px;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#111;max-width:640px">
<h1 style="font-size:22px;margin:0 0 4px">{{.Title}}</h1>
{{if .BriefURL}}<p style="margin:0 0 20px;font-size:13px"><a href="{{.BriefURL}}" style="color:#2f5fd0">Open in tempoBrief</a></p>{{end}}
{{if not .Sections}}<p>Nothing cleared the bar today.</p>{{end}}
{{range .Sections}}<h2 style="font-size:15px;text-transform:uppercase;letter-spacing:.04em;color:#555;margin:28px 0 8px;border-bottom:1px solid #ddd;padding-bottom:4px">{{.Topic}}</h2>
{{range .Stories}}<div style="margin:0 0 16px">
<a href="{{.URL}}" style="font-size:16px;font-weight:600;color:#111;text-decoration:none">{{.Title}}</a>
{{if .Summary}}<div style="font-size:14px;line-height:1.45;margin-top:2px">{{.Summary}}</div>{{end}}
<div style="font-size:12px;color:#666;margin-top:3px">{{score .Score}}/10 · {{sources .Sources}}{{if .Why}} · {{.Why}}{{end}}{{if .UpURL}} · <a href="{{.UpURL}}" style="color:#666">👍</a> <a href="{{.DownURL}}" style="color:#666">👎</a>{{end}}</div>
</div>{{end}}{{end}}
</body></html>`))

func emailText(m Message) string {
	var b strings.Builder
	b.WriteString(m.Title + "\n\n")
	for _, sec := range m.Sections {
		b.WriteString(strings.ToUpper(sec.Topic) + "\n\n")
		for _, st := range sec.Stories {
			fmt.Fprintf(&b, "%s\n%s\n%s/10 · %s\n%s\n\n", st.Title, st.Summary, scoreLabel(st.Score), sourcesLabel(st.Sources), st.URL)
		}
	}
	if len(m.Sections) == 0 {
		b.WriteString("Nothing cleared the bar today.\n")
	}
	return b.String()
}

func sendEmail(ctx context.Context, c settings.Channel, m Message) error {
	var html bytes.Buffer
	if err := emailHTML.Execute(&html, m); err != nil {
		return err
	}
	boundary := randomBoundary()
	var msg bytes.Buffer
	subject := m.Title
	if m.Kind == KindAlert && len(m.Stories()) > 0 {
		subject = m.Stories()[0].Title
	}
	headers := [][2]string{
		{"From", c.EmailFrom},
		{"To", c.EmailTo},
		{"Subject", mime.QEncoding.Encode("utf-8", subject)},
		{"Date", time.Now().Format(time.RFC1123Z)},
		{"MIME-Version", "1.0"},
		{"Content-Type", `multipart/alternative; boundary="` + boundary + `"`},
	}
	for _, h := range headers {
		fmt.Fprintf(&msg, "%s: %s\r\n", h[0], h[1])
	}
	fmt.Fprintf(&msg, "\r\n--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, emailText(m))
	fmt.Fprintf(&msg, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n--%s--\r\n", boundary, html.String(), boundary)

	port := c.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(c.SMTPHost, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	var err error
	if port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: c.SMTPHost})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(time.Minute))
	}
	client, err := smtp.NewClient(conn, c.SMTPHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer client.Close()
	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: c.SMTPHost}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}
	if c.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", c.SMTPUsername, c.SMTPPassword, c.SMTPHost)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(address(c.EmailFrom)); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	for _, to := range strings.Split(c.EmailTo, ",") {
		if err := client.Rcpt(address(to)); err != nil {
			return fmt.Errorf("smtp to %s: %w", strings.TrimSpace(to), err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg.Bytes()); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// address extracts "a@b" from "Name <a@b>".
func address(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		return strings.TrimSuffix(s[i+1:], ">")
	}
	return s
}

func randomBoundary() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "tb" + hex.EncodeToString(b)
}
