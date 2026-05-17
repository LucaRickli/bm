package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/lucarickli/bm/internal/extract"
	"github.com/lucarickli/bm/internal/release"
	"github.com/lucarickli/bm/internal/signature"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

func init() {
	syncCmd.Flags().StringP("config", "c", "releases.yaml", "Path to the configuration file")
	if err := viper.BindPFlag("sync.config", syncCmd.Flags().Lookup("config")); err != nil {
		panic(err)
	}

	syncCmd.Flags().StringSlice("only", nil, "Process only specified release IDs (comma-separated)")
	if err := viper.BindPFlag("sync.only", syncCmd.Flags().Lookup("only")); err != nil {
		panic(err)
	}

	syncCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	if err := viper.BindPFlag("sync.dry-run", syncCmd.Flags().Lookup("dry-run")); err != nil {
		panic(err)
	}

	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync binary releases defined in the configuration file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSync(
			cmd.Context(),
			viper.GetString("sync.config"),
			viper.GetStringSlice("sync.only"),
			viper.GetBool("sync.dry-run"),
		)
	},
}

func runSync(ctx context.Context, configFile string, only []string, dryRun bool) error {
	cfg, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("load config %q: %w", configFile, err)
	}

	filter := make(map[string]bool, len(only))
	for _, id := range only {
		if id != "" {
			filter[id] = true
		}
	}

	s := &syncer{client: &http.Client{}, dryRun: dryRun}

	log.Info().
		Str("config", configFile).
		Int("releases", len(cfg.Releases)).
		Bool("dry_run", dryRun).
		Msg("Starting sync")

	var failed int
	for _, r := range cfg.Releases {
		if len(filter) > 0 && !filter[r.ID] {
			log.Debug().Str("id", r.ID).Msg("Skipping (not in --only filter)")
			continue
		}
		if err := s.processRelease(ctx, r); err != nil {
			log.Error().Err(err).Str("id", r.ID).Msg("Release failed")
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d release(s) failed", failed)
	}

	log.Info().Msg("Sync completed")
	return nil
}

type syncer struct {
	client *http.Client
	dryRun bool
}

func (s *syncer) processRelease(ctx context.Context, r release.Release) error {
	if _, err := os.Stat(r.Dst); os.IsNotExist(err) {
		return fmt.Errorf("destination %q does not exist", r.Dst)
	}

	vars := extractVars(r.Src, r.Regex)

	if err := expandTemplates(&r, vars); err != nil {
		return err
	}

	log.Info().Str("id", r.ID).Str("src", r.Src).Str("dst", r.Dst).Msg("Processing")

	if s.dryRun {
		s.logDryRun(r)
		return nil
	}

	workDir, err := os.MkdirTemp("", "bm-"+r.ID+"-*")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	log.Debug().Str("id", r.ID).Str("work_dir", workDir).Msg("Work directory created")

	downloaded, err := s.downloadToFile(ctx, r.Src, workDir)
	if err != nil {
		return fmt.Errorf("download %s: %w", r.Src, err)
	}
	defer downloaded.Close()

	if r.Integrity != nil {
		if err := s.verifySignature(ctx, r, downloaded); err != nil {
			return fmt.Errorf("signature verification: %w", err)
		}
		log.Info().Str("id", r.ID).Msg("Signature verified")
	}

	if r.Unpack != "" {
		if err := s.extractArchive(r, downloaded, workDir); err != nil {
			return fmt.Errorf("extract archive: %w", err)
		}
	}

	return s.installAssets(r, workDir)
}

// downloadToFile fetches url and saves it to a file in dir, returning the open file seeked to the start.
func (s *syncer) downloadToFile(ctx context.Context, rawURL, dir string) (*os.File, error) {
	resp, err := s.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	f, err := os.Create(filepath.Join(dir, path.Base(u.Path)))
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return nil, err
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}

	return f, nil
}

// verifySignature verifies the downloaded file against the integrity configuration.
// After returning, f is seeked back to the start so it can be used again.
func (s *syncer) verifySignature(ctx context.Context, r release.Release, f *os.File) error {
	fileData, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek file: %w", err)
	}

	switch r.Integrity.Algorithm {
	case release.AlgorithmBundle:
		if r.Integrity.Bundle == "" {
			log.Warn().Str("id", r.ID).Msg("Integrity block has algorithm=bundle but no bundle URL - skipping verification")
			return nil
		}
		log.Debug().Str("id", r.ID).Str("bundle", r.Integrity.Bundle).Msg("Verifying bundle signature")
		bundleData, err := s.fetchBytes(ctx, r.Integrity.Bundle)
		if err != nil {
			return fmt.Errorf("fetch bundle: %w", err)
		}
		return signature.VerifyBundle(bundleData, fileData)

	case release.AlgorithmSHA256:
		if r.Integrity.Signature == "" {
			log.Warn().Str("id", r.ID).Msg("Integrity block has algorithm=sha256 but no checksum URL - skipping verification")
			return nil
		}
		log.Debug().Str("id", r.ID).Str("checksum", r.Integrity.Signature).Msg("Verifying SHA256 checksum")
		checksumData, err := s.fetchBytes(ctx, r.Integrity.Signature)
		if err != nil {
			return fmt.Errorf("fetch checksum: %w", err)
		}
		return signature.VerifySHA256(checksumData, fileData, filepath.Base(f.Name()))

	case release.AlgorithmPGP, release.AlgorithmCosign:
		if r.Integrity.PubKey == "" || r.Integrity.Signature == "" {
			log.Warn().Str("id", r.ID).Msg("Integrity block present but key or signature URL is missing - skipping verification")
			return nil
		}
		keyData, err := s.fetchBytes(ctx, r.Integrity.PubKey)
		if err != nil {
			return fmt.Errorf("fetch public key: %w", err)
		}
		sigData, err := s.fetchBytes(ctx, r.Integrity.Signature)
		if err != nil {
			return fmt.Errorf("fetch signature: %w", err)
		}
		log.Debug().
			Str("id", r.ID).
			Str("key", r.Integrity.PubKey).
			Str("signature", r.Integrity.Signature).
			Msg("Verifying signature")
		if r.Integrity.Algorithm == release.AlgorithmCosign {
			return signature.VerifyCosign(keyData, sigData, fileData)
		}
		return signature.VerifyPGP(keyData, sigData, fileData)

	default:
		return fmt.Errorf("unsupported algorithm %q", r.Integrity.Algorithm)
	}
}

func (s *syncer) extractArchive(r release.Release, f *os.File, dir string) error {
	log.Debug().Str("id", r.ID).Str("format", string(r.Unpack)).Msg("Extracting archive")

	switch r.Unpack {
	case release.UnpackTarGz:
		return extract.Tar(f, dir, true)
	case release.UnpackTar:
		return extract.Tar(f, dir, false)
	case release.UnpackZip:
		return extract.Zip(f, dir)
	default:
		return fmt.Errorf("unsupported unpack format %q", r.Unpack)
	}
}

func (s *syncer) installAssets(r release.Release, workDir string) error {
	for _, asset := range r.Assets {
		src := filepath.Join(workDir, filepath.FromSlash(asset.Src))
		dst := filepath.Join(r.Dst, asset.Dst)

		log.Debug().Str("id", r.ID).Str("src", src).Str("dst", dst).Msg("Installing asset")

		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("install %s -> %s: %w", asset.Src, asset.Dst, err)
		}

		log.Info().Str("id", r.ID).Str("asset", asset.Dst).Str("dst", dst).Msg("Asset installed")
	}
	return nil
}

func (s *syncer) logDryRun(r release.Release) {
	log.Info().Str("id", r.ID).Str("url", r.Src).Msg("[dry-run] Would download")
	if r.Integrity != nil {
		switch r.Integrity.Algorithm {
		case release.AlgorithmBundle:
			if r.Integrity.Bundle != "" {
				log.Info().Str("id", r.ID).Str("bundle", r.Integrity.Bundle).Msg("[dry-run] Would verify bundle signature")
			}
		case release.AlgorithmSHA256:
			if r.Integrity.Signature != "" {
				log.Info().Str("id", r.ID).Str("checksum", r.Integrity.Signature).Msg("[dry-run] Would verify SHA256 checksum")
			}
		default:
			if r.Integrity.PubKey != "" && r.Integrity.Signature != "" {
				alg := r.Integrity.Algorithm
				if alg == "" {
					alg = release.AlgorithmPGP
				}
				log.Info().Str("id", r.ID).Str("algorithm", string(alg)).Msg("[dry-run] Would verify signature")
			}
		}
	}
	if r.Unpack != "" {
		log.Info().Str("id", r.ID).Str("format", string(r.Unpack)).Msg("[dry-run] Would extract archive")
	}
	for _, asset := range r.Assets {
		log.Info().
			Str("id", r.ID).
			Str("src", asset.Src).
			Str("dst", filepath.Join(r.Dst, asset.Dst)).
			Msg("[dry-run] Would install asset")
	}
}

func (s *syncer) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s returned %s", rawURL, resp.Status)
	}
	return resp, nil
}

func (s *syncer) fetchBytes(ctx context.Context, rawURL string) ([]byte, error) {
	resp, err := s.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func loadConfig(configFile string) (*release.Config, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg release.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	log.Debug().Str("path", configFile).Int("releases", len(cfg.Releases)).Msg("Config loaded")
	return &cfg, nil
}

// extractVars applies regex to src and returns a map of named capture group values.
func extractVars(src string, regex *regexp.Regexp) map[string]string {
	if regex == nil {
		return nil
	}

	matches := regex.FindStringSubmatch(src)
	if matches == nil {
		return nil
	}

	vars := make(map[string]string, len(regex.SubexpNames()))
	for i, name := range regex.SubexpNames() {
		if i != 0 && name != "" {
			vars[name] = matches[i]
			log.Debug().Str("var", name).Str("value", matches[i]).Msg("Extracted template var")
		}
	}

	return vars
}

// expandTemplates renders Go template syntax in src, asset paths, and Integrity URLs
// using vars extracted by extractVars.
func expandTemplates(r *release.Release, vars map[string]string) error {
	if len(vars) == 0 {
		return nil
	}

	var err error

	r.Src, err = renderTemplate(r.Src, vars)
	if err != nil {
		return fmt.Errorf("expand src: %w", err)
	}

	for i := range r.Assets {
		r.Assets[i].Src, err = renderTemplate(r.Assets[i].Src, vars)
		if err != nil {
			return fmt.Errorf("expand assets[%d].src: %w", i, err)
		}
		r.Assets[i].Dst, err = renderTemplate(r.Assets[i].Dst, vars)
		if err != nil {
			return fmt.Errorf("expand assets[%d].dst: %w", i, err)
		}
	}

	if r.Integrity != nil {
		r.Integrity.PubKey, err = renderTemplate(r.Integrity.PubKey, vars)
		if err != nil {
			return fmt.Errorf("expand Integrity.key: %w", err)
		}
		r.Integrity.Signature, err = renderTemplate(r.Integrity.Signature, vars)
		if err != nil {
			return fmt.Errorf("expand Integrity.signature: %w", err)
		}
		r.Integrity.Bundle, err = renderTemplate(r.Integrity.Bundle, vars)
		if err != nil {
			return fmt.Errorf("expand Integrity.bundle: %w", err)
		}
	}

	return nil
}

func renderTemplate(text string, vars map[string]string) (string, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}

	tmpl, err := template.New("").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", text, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute %q: %w", text, err)
	}

	return buf.String(), nil
}

// copyFile copies src to dst, preserving permissions and ensuring the execute bit is set.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()|0o111)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
