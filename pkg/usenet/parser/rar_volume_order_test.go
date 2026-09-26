package parser

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/nntp"
	"github.com/sirrobot01/decypharr/pkg/usenet/manifest"
)

// memoryArticles serves decoded article bodies from memory.
type memoryArticles map[string][]byte

func (m memoryArticles) Header(context.Context, string, int) (*nntp.YencMetadata, error) {
	return nil, fmt.Errorf("headers are not served")
}

func (m memoryArticles) Body(_ context.Context, messageID string) ([]byte, error) {
	body, ok := m[messageID]
	if !ok {
		return nil, fmt.Errorf("article %s not found", messageID)
	}
	return body, nil
}

func (m memoryArticles) Stat(context.Context, string) error { return nil }
func (m memoryArticles) IsAvailable(string) bool            { return true }
func (m memoryArticles) Metrics() ArticleMetrics            { return ArticleMetrics{} }

// rarPayload regenerates payload.bin, the file stored in testdata/rar5_volumes.
func rarPayload() []byte {
	payload := make([]byte, 6000)
	x := uint32(1)
	for i := range payload {
		x = x*1664525 + 1013904223
		payload[i] = byte(x >> 24)
	}
	return payload
}

// TestRARProcessAssemblesVolumesInArchiveOrder pins that a stored RAR5 file
// assembles byte-for-byte even when obfuscated volumes were posted out of
// sequence: the order comes from the main-header volume numbers, not from the
// upload order.
//
// testdata/rar5_volumes was made with `rar a -ma5 -m0 -v2000b -ep movie.rar
// payload.bin`. As the RAR5 format specifies, the first volume's main header
// carries no volume number and the others carry 1, 2 and 3.
func TestRARProcessAssemblesVolumesInArchiveOrder(t *testing.T) {
	const segmentSize = 512
	volumes := make([][]byte, 4)
	for i := range volumes {
		data, err := os.ReadFile(filepath.Join("testdata", "rar5_volumes", fmt.Sprintf("movie.part%d.rar", i+1)))
		if err != nil {
			t.Fatal(err)
		}
		volumes[i] = data
	}

	for _, tc := range []struct {
		name      string
		filenames []string // per archive volume
		upload    []int    // archive volume at each upload position
	}{
		{
			name:      "named volumes in order",
			filenames: []string{"movie.part1.rar", "movie.part2.rar", "movie.part3.rar", "movie.part4.rar"},
			upload:    []int{0, 1, 2, 3},
		},
		{
			name:      "obfuscated volumes posted out of order",
			filenames: []string{"f81c2d0a", "0b7e59c4", "c4a1e7f3", "9d3b6a25"},
			upload:    []int{2, 0, 3, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			articles := memoryArticles{}
			group := &FileGroup{BaseName: "movie", Groups: map[string]struct{}{"alt.test": {}}, fileMeta: map[string]filePartMeta{}}
			for position, volume := range tc.upload {
				data := volumes[volume]
				file := manifest.File{Filename: tc.filenames[volume], Number: position + 1, Groups: []string{"alt.test"}}
				for offset, number := 0, 1; offset < len(data); offset, number = offset+segmentSize, number+1 {
					end := min(offset+segmentSize, len(data))
					messageID := fmt.Sprintf("v%d-s%d@test", volume, number)
					articles[messageID] = data[offset:end]
					file.Segments = append(file.Segments, manifest.Segment{Number: number, MessageID: messageID, Bytes: int64(end - offset)})
				}
				group.fileMeta[fileMetaKey(file)] = filePartMeta{fileSize: int64(len(data)), segmentSize: segmentSize}
				group.Files = append(group.Files, file)
			}

			files, err := NewRARParser(articles, 4, zerolog.Nop()).Process(t.Context(), group, "")
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if len(files) != 1 || files[0].Name != "payload.bin" {
				t.Fatalf("files = %+v, want only payload.bin", files)
			}
			var assembled bytes.Buffer
			for _, segment := range files[0].Segments {
				body := articles[segment.MessageID]
				assembled.Write(body[segment.SegmentDataStart : segment.SegmentDataStart+segment.Bytes])
			}
			if !bytes.Equal(assembled.Bytes(), rarPayload()) {
				t.Fatalf("assembled %d bytes that differ from the stored payload", assembled.Len())
			}
		})
	}
}
