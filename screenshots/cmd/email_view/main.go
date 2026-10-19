// email_view is a small helper that renders a mock email with inline images
// for screenshot generation. It creates a bubbletea program displaying a
// realistic HTML email using the real EmailView component.
package main

import (
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/fetcher"
	"github.com/floatpane/matcha/tui"
)

func main() {
	email := fetcher.Email{
		UID:       1001,
		From:      "Sarah Chen <sarah.chen@example.com>",
		To:        []string{"team@example.com"},
		Subject:   "New Dashboard Redesign - Preview & Feedback",
		Date:      time.Now().Add(-2 * time.Hour),
		MessageID: "<dashboard-redesign-001@example.com>",
		AccountID: "demo-user",
		Body: `<html>
<body>
<h1>Dashboard Redesign Preview</h1>

<p>Hi team,</p>

<p>I'm excited to share the <b>new dashboard redesign</b> we've been working on!
Here's a quick overview of the changes:</p>

<h2>What's New</h2>

<ul>
<li><b>Simplified navigation</b> - We reduced sidebar items from 12 to 6</li>
<li><b>Dark mode support</b> - Full theme switching with system preference detection</li>
<li><b>Real-time updates</b> - WebSocket integration for live data refresh</li>
<li><b>Responsive layout</b> - Optimized for mobile, tablet, and desktop</li>
</ul>

<h2>Screenshots</h2>

<p>Here's the new main view:</p>
<img src="cid:dashboard-main" alt="Dashboard main view" />

<p>And the analytics panel:</p>
<img src="cid:analytics-panel" alt="Analytics panel with charts" />

<h2>Next Steps</h2>

<ol>
<li>Review the mockups above</li>
<li>Leave comments in the <a href="https://figma.com/file/example">Figma file</a></li>
<li>Join the feedback session on <b>Thursday at 3pm</b></li>
</ol>

<p>Looking forward to your thoughts!</p>

<p>Best,<br/>
Sarah</p>
</body>
</html>`,
		Attachments: []fetcher.Attachment{
			{
				Filename: "dashboard-mockup.png",
				MIMEType: "image/png",
				Inline:   false,
			},
			{
				Filename: "analytics-export.csv",
				MIMEType: "text/csv",
				Inline:   false,
			},
		},
	}

	ev := tui.NewEmailView(email, 140, 45, tui.MailboxInbox, true)

	p := tea.NewProgram(ev)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
