package gopeed

import (
	"context"
	"encoding/json"

	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type engineCompletion struct {
	Status   string `json:"status"`
	Protocol string `json:"protocol"`
	Meta     struct {
		Res struct {
			Size  *int64 `json:"size"`
			Range bool   `json:"range"`
		} `json:"res"`
	} `json:"meta"`
	Progress struct {
		Downloaded *int64 `json:"downloaded"`
	} `json:"progress"`
}

// Gopeed 1.9.3 can return done after a non-range body read times out. Its
// onDone overwrites progress.downloaded with the advertised resource size,
// while the preallocated file already has that size. Only the HTTP fetcher's
// /stats counters retain the actual written bytes and connection completion.
// A range resource that the resolve request prefetched in full is written
// without any connection; that is the only way a done HTTP task lists none, since
// a non-range body always reads through connection 0 and connections persist.
func (s *Service) verifyCompletion(ctx context.Context, id string, task engineCompletion, size int64) error {
	if task.Protocol != "http" || task.Meta.Res.Size == nil || task.Progress.Downloaded == nil ||
		*task.Meta.Res.Size != size || *task.Progress.Downloaded != size || size < 0 || size > delegate.MaxMoveBytes {
		return refused("download_incomplete")
	}
	reply, err := s.engine(ctx, "GET", "/api/v1/tasks/"+id+"/stats", nil, nil)
	if err != nil || reply.Code != 0 {
		// A transient engine/IPC failure is not proof of corrupt content. Wait
		// for a later polling pass; never admit a move without this evidence.
		return refused("engine_unavailable")
	}
	var stats struct {
		Connections []struct {
			Downloaded *int64 `json:"downloaded"`
			Completed  *bool  `json:"completed"`
			Failed     *bool  `json:"failed"`
		} `json:"connections"`
	}
	if json.Unmarshal(reply.Data, &stats) != nil || len(stats.Connections) > 128 {
		return refused("download_incomplete")
	}
	if len(stats.Connections) == 0 {
		if task.Meta.Res.Range {
			return nil
		}
		return refused("download_incomplete")
	}
	var written int64
	for _, c := range stats.Connections {
		if c.Downloaded == nil || c.Completed == nil || c.Failed == nil ||
			*c.Downloaded < 0 || *c.Downloaded > size-written || !*c.Completed || *c.Failed {
			return refused("download_incomplete")
		}
		written += *c.Downloaded
	}
	if written != size {
		return refused("download_incomplete")
	}
	return nil
}

func (s *Service) recordIncompleteDownload(ctx context.Context, id string, job Job) (any, error) {
	// No executor reference or replayable admission exists. Preserve the private
	// bytes for diagnostics and show a durable failure, including after restart.
	job.SaveError = "download_incomplete"
	job.SaveState = "download_failed"
	if err := s.recordSave(ctx, id, job); err != nil {
		return nil, err
	}
	return nil, refused(job.SaveError)
}
