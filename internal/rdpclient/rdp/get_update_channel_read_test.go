package rdp

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rcarmo/womprat/internal/rdpclient/protocol/audio"
	"github.com/rcarmo/womprat/internal/rdpclient/protocol/drdynvc"
)

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }

func TestGetX224UpdatePropagatesChannelReadErrors(t *testing.T) {
	tests := []struct {
		name        string
		channelName string
		channelID   uint16
		client      *Client
		want        string
	}{
		{
			name:        "audio",
			channelName: audio.ChannelRDPSND,
			channelID:   1007,
			client:      &Client{audioHandler: &AudioHandler{}},
			want:        "audio channel read",
		},
		{
			name:        "drdynvc",
			channelName: drdynvc.ChannelName,
			channelID:   1008,
			client:      &Client{displayControl: &DisplayControlHandler{}},
			want:        "drdynvc channel read",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.client
			client.channelIDMap = map[string]uint16{tt.channelName: tt.channelID, "global": 1003}
			client.mcsLayer = &MockMCSLayer{ReceiveFunc: func() (uint16, io.Reader, error) {
				return tt.channelID, failingReader{}, nil
			}}
			_, err := client.getX224Update()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("getX224Update error = %v, want %q", err, tt.want)
			}
		})
	}
}
