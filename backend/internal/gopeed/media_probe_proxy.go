package gopeed

import (
	"context"
	"path/filepath"

	"dsmapp/internal/media"

	"github.com/synopackageland/syno-pkg-land-sdk/outbound"
)

// inspectMedia runs one probe through the SDK-owned public network boundary.
func (s *Service) inspectMedia(ctx context.Context, raw string) (media.Info, error) {
	if !safeURL(raw) || !s.allowedURL(raw) {
		return media.Info{}, refused("url_denied")
	}
	env := s.mediaEnv()
	if env.YtDlp == "" && env.GalleryDlp == "" {
		return media.Probe(ctx, s.mediaExec(), env, raw)
	}
	if err := media.PrepareHome(env.Home); err != nil {
		return media.Info{Kind: "http"}, nil
	}
	proxy, err := outbound.StartProbeProxy(ctx, outbound.ProbeProxyOptions{AllowURL: s.allowedURL})
	if err != nil {
		return media.Info{Kind: "http"}, nil
	}
	defer proxy.Close()
	run := s.mediaExec()
	guarded := func(_ context.Context, name string, args, environ []string, max int64) ([]byte, []byte, error) {
		// Insert before the positional URL terminator, preserving closed argv.
		options := []string{"--proxy", proxy.URL()}
		if name != env.GalleryDlp {
			options = append(options, "--geo-verification-proxy", proxy.URL())
		}
		args = append(append(append([]string{}, args[:len(args)-2]...), options...), args[len(args)-2:]...)
		return run(proxy.Context(), name, args, environ, max)
	}
	info, err := media.Probe(ctx, guarded, env, raw)
	if proxy.Denied() {
		return media.Info{}, refused("url_denied")
	}
	if ctx.Err() != nil {
		return media.Info{}, ctx.Err()
	}
	if proxy.Context().Err() != nil {
		return media.Info{Kind: "http"}, nil
	}
	return info, err
}

// Authentication material is scoped to this caller and this single probe.
func (s *Service) inspectOwnerMedia(ctx context.Context, owner, raw string) (media.Info, error) {
	cookies, cleanup, err := s.cookies().Copy(owner, raw, filepath.Join(s.mediaHome(), "tmp"))
	if err != nil {
		return media.Info{}, mediaRefused(err)
	}
	defer cleanup()
	if cookies == "" {
		return s.inspectMedia(ctx, raw)
	}
	clone := *s
	run := s.mediaExec()
	clone.MediaExec = func(ctx context.Context, name string, args, env []string, max int64) ([]byte, []byte, error) {
		args = append(append(append([]string{}, args[:len(args)-2]...), "--cookies", cookies), args[len(args)-2:]...)
		return run(ctx, name, args, env, max)
	}
	return clone.inspectMedia(ctx, raw)
}
