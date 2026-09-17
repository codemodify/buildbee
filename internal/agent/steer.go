package agent

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/agent/acp"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/gorilla/websocket"
)

// listen follows a Run's events on the Server and passes people's
// messages for the agent to out until ctx ends, reconnecting as needed.
// after is the Run's last event when it was claimed: earlier messages were
// already folded into the prompt.
func (w *Agent) listen(ctx context.Context, runID string, after int, out chan<- acp.Steer) {
	u := strings.Replace(w.api.base, "http", "ws", 1) + "/v1/runs/" + runID + "/ws"
	backoff := time.Second
	for ctx.Err() == nil {
		hdr := http.Header{"X-BuildBee-Agent": {w.cfg.Name}, "X-BuildBee-Bot": {w.cfg.BotID}}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, u+"?after="+strconv.Itoa(after), hdr)
		if err != nil {
			sleep(ctx, backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		for {
			var ev models.RunEvent
			if err := conn.ReadJSON(&ev); err != nil {
				break
			}
			after = max(after, ev.Seq)
			if ev.Kind != models.RunEventSteer {
				continue
			}
			if queued, _ := ev.Payload["queued"].(bool); queued {
				continue // it is in the prompt
			}
			by, _ := ev.Payload["by"].(string)
			text, _ := ev.Payload["text"].(string)
			interrupt, _ := ev.Payload["interrupt"].(bool)
			select {
			case out <- acp.Steer{By: by, Text: text, Interrupt: interrupt}:
			case <-ctx.Done():
			}
		}
		stop()
		conn.Close()
	}
}
