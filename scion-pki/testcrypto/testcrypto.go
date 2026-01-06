// Copyright 2020 Anapaya Systems
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testcrypto

import (
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/private/serrors"
	"github.com/scionproto/scion/pkg/private/util"
	"github.com/scionproto/scion/pkg/scrypto/cppki"
	"github.com/scionproto/scion/private/app/command"
	"github.com/scionproto/scion/scion-pki/certs"
	"github.com/scionproto/scion/scion-pki/conf"
	"github.com/scionproto/scion/scion-pki/key"
	"github.com/scionproto/scion/scion-pki/trcs"
)

func Cmd(pather command.Pather) *cobra.Command {
	var flags struct {
		topo       string
		out        string
		noCleanup  bool
		isdDir     bool
		asValidity string
	}

	cmd := &cobra.Command{
		Use:     "testcrypto",
		Short:   "Generate crypto material for test topology",
		Example: fmt.Sprintf(`  %[1]s testcrypto -t testing.topo -o gen`, pather.CommandPath()),
		Hidden:  true,
		Long: `'testcrypto' generates the crypto material for a test topology.

This command should only be used in testing.
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			asValidity, err := util.ParseDuration(flags.asValidity)
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			return testcrypto(
				flags.topo,
				flags.out,
				flags.noCleanup,
				flags.isdDir,
				asValidity,
				cmd.OutOrStdout(),
			)
		},
	}

	cmd.Flags().StringVarP(&flags.topo, "topo", "t", "", "Topology description file (required)")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "gen", "Output directory")
	cmd.Flags().BoolVar(&flags.isdDir, "isd-dir", false, "Group ASes in ISD directory")
	cmd.Flags().StringVar(&flags.asValidity, "as-validity", "3d", "AS certificate validity")
	cmd.MarkFlagRequired("topo")

	cmd.AddCommand(newUpdate())

	return cmd
}

type config struct {
	topo       topo
	out        outConfig
	now        time.Time
	asValidity time.Duration
	writer     io.Writer
}

type membershipView struct {
	IA            addr.IA
	Core          bool
	Issuing       bool
	Voting        bool
	Authoritative bool
	CA            addr.IA
}

func membershipViews(base addr.IA, baseAttrs PrivateISDMembershipAttrs, priv []PrivateISDMembership, baseCA addr.IA) []membershipView {
	views := make([]membershipView, 0, len(priv)+1)
	views = append(views, membershipView{
		IA:            base,
		Core:          baseAttrs.Core,
		Issuing:       baseAttrs.Issuing,
		Voting:        baseAttrs.Voting,
		Authoritative: baseAttrs.Authoritative,
		CA:            baseCA,
	})
	for _, p := range priv {
		ia, err := addr.IAFrom(p.ISD, base.AS())
		if err != nil {
			panic(fmt.Sprintf("creating IA from ISD %d and AS %s: %v", p.ISD, base.AS(), err))
		}
		if ia == base {
			// Skip duplicate of base membership (private_only_as case).
			continue
		}
		ca := baseCA
		if !p.CertIssuer.IsZero() {
			ca = p.CertIssuer
		}
		views = append(views, membershipView{
			IA:            ia,
			Core:          p.Core,
			Issuing:       p.Issuing,
			Voting:        p.Voting,
			Authoritative: p.Authoritative,
			CA:            ca,
		})
	}
	return views
}

type PrivateISDMembershipAttrs struct {
	Core          bool
	Issuing       bool
	Voting        bool
	Authoritative bool
}

func testcrypto(
	topo string,
	outDir string,
	noCleanup bool,
	isdDir bool,
	asValidity time.Duration,
	writer io.Writer,
) error {

	t, err := loadTopo(topo)
	if err != nil {
		return err
	}
	out := outConfig{
		base: outDir,
		isd:  isdDir,
	}
	if err := prepareDirectories(t, out); err != nil {
		return err
	}

	cfg := config{
		topo:       t,
		out:        out,
		now:        time.Now().Add(-time.Minute),
		asValidity: asValidity,
		writer:     writer,
	}

	if err := setupTemplates(cfg); err != nil {
		return err
	}
	if err := createVoters(cfg); err != nil {
		return err
	}
	if err := createCAs(cfg); err != nil {
		return err
	}
	if err := createASes(cfg); err != nil {
		return err
	}
	if err := createTRCs(cfg); err != nil {
		return err
	}
	if err := flatten(out); err != nil {
		return err
	}
	if !noCleanup {
		if err := cleanup(cfg); err != nil {
			return err
		}
	}
	return nil
}

func createVoters(cfg config) error {
	for ia, d := range cfg.topo.ASes {
		baseAttrs := cfg.topo.baseAttrs(ia)
		for _, view := range membershipViews(ia, baseAttrs, d.PrivateISDs, d.CA) {
			if !view.Voting {
				continue
			}
			fmt.Fprintf(cfg.writer, "Generate sensitive and regular voting certificate for %s\n", view.IA)
			votingDir := cryptoVotingDir(view.IA, cfg.out)

			cmd := certs.Cmd(command.StringPather("certificate"))
			cmd.SetArgs([]string{
				"create",
				sensitiveVotingTemplatePath(view.IA, cfg.out),
				filepath.Join(votingDir, sensitiveCertName(view.IA)),
				filepath.Join(votingDir, sensitiveKeyName(view.IA)),
				"--profile=sensitive-voting",
				"--not-before=" + strconv.Itoa(int(cfg.now.Unix())),
				"--not-after=730d",
			})
			if err := cmd.Execute(); err != nil {
				return err
			}
			err := copyFile(
				filepath.Join(votingDir, "sensitive-voting.crt"),
				filepath.Join(votingDir, sensitiveCertName(view.IA)),
			)
			if err != nil {
				return err
			}
			// Backward-compatible fallback: create generic filename if absent - only for the public isd membership (for tests etc)
			if _, err := os.Stat(filepath.Join(votingDir, "sensitive-voting.key")); errors.Is(err, os.ErrNotExist) {
				if err := copyFile(
					filepath.Join(votingDir, "sensitive-voting.key"),
					filepath.Join(votingDir, sensitiveKeyName(view.IA)),
				); err != nil {
					return err
				}
			}

			cmd = certs.Cmd(command.StringPather("certificate"))
			cmd.SetArgs([]string{
				"create",
				regularVotingTemplatePath(view.IA, cfg.out),
				filepath.Join(votingDir, regularCertName(view.IA)),
				filepath.Join(votingDir, regularKeyName(view.IA)),
				"--profile=regular-voting",
				"--not-before=" + strconv.Itoa(int(cfg.now.Unix())),
				"--not-after=730d",
			})
			if err := cmd.Execute(); err != nil {
				return err
			}
			err = copyFile(
				filepath.Join(votingDir, "regular-voting.crt"),
				filepath.Join(votingDir, regularCertName(view.IA)),
			)
			if err != nil {
				return err
			}
			// Backward-compatible fallback: create generic filename if absent - only for the public isd membership (for tests etc)
			if _, err := os.Stat(filepath.Join(votingDir, "regular-voting.key")); errors.Is(err, os.ErrNotExist) {
				if err := copyFile(
					filepath.Join(votingDir, "regular-voting.key"),
					filepath.Join(votingDir, regularKeyName(view.IA)),
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func createCAs(cfg config) error {
	for ia, d := range cfg.topo.ASes {
		baseAttrs := cfg.topo.baseAttrs(ia)
		for _, view := range membershipViews(ia, baseAttrs, d.PrivateISDs, d.CA) {
			if !view.Issuing {
				continue
			}
			fmt.Fprintf(cfg.writer, "Generate CP Root and CP CA certificate for %s\n", view.IA)
			caDir := cryptoCADir(view.IA, cfg.out)

			cmd := certs.Cmd(command.StringPather("certificate"))
			cmd.SetArgs([]string{
				"create",
				cpRootTemplatePath(view.IA, cfg.out),
				filepath.Join(caDir, rootCertName(view.IA)),
				filepath.Join(caDir, cpRootKeyName(view.IA)),
				"--profile=cp-root",
				"--not-before=" + strconv.Itoa(int(cfg.now.Unix())),
				"--not-after=730d",
			})
			if err := cmd.Execute(); err != nil {
				return err
			}
			// Backward-compatible fallback: create generic filename if absent - only for the public isd membership (for tests etc)
			if _, err := os.Stat(filepath.Join(caDir, "cp-root.key")); errors.Is(err, os.ErrNotExist) {
				if err := copyFile(
					filepath.Join(caDir, "cp-root.key"),
					filepath.Join(caDir, cpRootKeyName(view.IA)),
				); err != nil {
					return err
				}
			}

			cmd = certs.Cmd(command.StringPather("certificate"))
			cmd.SetArgs([]string{
				"create",
				cpCATemplatePath(view.IA, cfg.out),
				filepath.Join(caDir, caCertName(view.IA)),
				filepath.Join(caDir, cpCAKeyName(view.IA)),
				"--profile=cp-ca",
				"--not-before=" + strconv.Itoa(int(cfg.now.Unix())),
				"--not-after=700d",
				"--ca=" + filepath.Join(caDir, rootCertName(view.IA)),
				"--ca-key=" + filepath.Join(caDir, cpRootKeyName(view.IA)),
			})
			if err := cmd.Execute(); err != nil {
				return err
			}
			// Backward-compatible fallback: create generic filename if absent - only for the public isd membership (for tests etc)
			if _, err := os.Stat(filepath.Join(caDir, "cp-ca.key")); errors.Is(err, os.ErrNotExist) {
				if err := copyFile(
					filepath.Join(caDir, "cp-ca.key"),
					filepath.Join(caDir, cpCAKeyName(view.IA)),
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func createASes(cfg config) error {
	for ia, d := range cfg.topo.ASes {
		baseAttrs := cfg.topo.baseAttrs(ia)
		for _, view := range membershipViews(ia, baseAttrs, d.PrivateISDs, d.CA) {
			ca := view.CA
			fmt.Fprintf(cfg.writer, "Generate CP AS certificate for %s issued by %s\n", view.IA, ca)
			caDir := cryptoCADir(ca, cfg.out)
			asDir := cryptoASDir(view.IA, cfg.out)

			keyPath := filepath.Join(asDir, "cp-as.key")
			args := []string{
				"create",
				cpASTemplatePath(view.IA, cfg.out),
				filepath.Join(asDir, chainName(view.IA)),
			}
			if _, err := os.Stat(keyPath); err == nil {
				args = append(args, "--key", keyPath)
			} else if errors.Is(err, os.ErrNotExist) {
				args = append(args, keyPath)
			} else if err != nil {
				return err
			}
			args = append(args,
				"--profile=cp-as",
				"--not-before="+strconv.Itoa(int(cfg.now.Unix())),
				"--not-after="+util.FmtDuration(cfg.asValidity),
				"--ca="+filepath.Join(caDir, caCertName(ca)),
				"--ca-key="+preferCAKey(caDir, ca),
				"--bundle",
			)

			cmd := certs.Cmd(command.StringPather("certificate"))
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}

type voterInfo struct {
	sensitiveKey  crypto.Signer
	sensitiveCert *x509.Certificate
	regularKey    crypto.Signer
	regularCert   *x509.Certificate
}

type trcRoleSet struct {
	authoritatives map[addr.AS]struct{}
	cores          map[addr.AS]struct{}
	voters         map[addr.IA]struct{}
	certFiles      map[string]struct{}
}

func newTRCRoleSet() *trcRoleSet {
	return &trcRoleSet{
		authoritatives: map[addr.AS]struct{}{},
		cores:          map[addr.AS]struct{}{},
		voters:         map[addr.IA]struct{}{},
		certFiles:      map[string]struct{}{},
	}
}

func (s *trcRoleSet) addAuthoritative(as addr.AS) { s.authoritatives[as] = struct{}{} }
func (s *trcRoleSet) addCore(as addr.AS)          { s.cores[as] = struct{}{} }
func (s *trcRoleSet) addVoter(ia addr.IA)         { s.voters[ia] = struct{}{} }
func (s *trcRoleSet) addCert(path string)         { s.certFiles[path] = struct{}{} }

func createTRCs(cfg config) error {
	roleSets := make(map[addr.ISD]*trcRoleSet)
	ensureRoleSet := func(isd addr.ISD) *trcRoleSet {
		set, ok := roleSets[isd]
		if !ok {
			set = newTRCRoleSet()
			roleSets[isd] = set
		}
		return set
	}

	for ia, d := range cfg.topo.ASes {
		baseAttrs := cfg.topo.baseAttrs(ia)
		for _, view := range membershipViews(ia, baseAttrs, d.PrivateISDs, d.CA) {
			set := ensureRoleSet(view.IA.ISD())
			if view.Authoritative {
				set.addAuthoritative(ia.AS())
			}
			if view.Core {
				set.addCore(ia.AS())
			}
			if view.Issuing {
				set.addCert(filepath.Join(cryptoCADir(view.CA, cfg.out), rootCertName(view.CA)))
			}
			if view.Voting {
				set.addVoter(view.IA)
				set.addCert(filepath.Join(cryptoVotingDir(view.IA, cfg.out), regularCertName(view.IA)))
				set.addCert(filepath.Join(cryptoVotingDir(view.IA, cfg.out), sensitiveCertName(view.IA)))
			}
		}
	}

	for isd, set := range roleSets {
		voterList := sortIASet(set.voters)
		trcConf := conf.TRC{
			ISD:           isd,
			Description:   fmt.Sprintf("Testcrypto TRC for ISD %d", isd),
			SerialVersion: 1,
			BaseVersion:   1,
			VotingQuorum:  uint8(len(voterList)/2 + 1),
			Validity: conf.Validity{
				NotBefore: conf.Time(cfg.now.UTC()),
				Validity:  util.DurWrap{Duration: 450 * 24 * time.Hour},
			},
			CoreASes:          sortASSet(set.cores),
			AuthoritativeASes: sortASSet(set.authoritatives),
			CertificateFiles:  sortStringSet(set.certFiles),
		}
		trc, err := trcs.CreatePayload(trcConf, nil)
		if err != nil {
			return serrors.Wrap("creating TRC payload", err, "isd", isd)
		}
		raw, err := trc.Encode()
		if err != nil {
			return serrors.Wrap("encoding TRC payload", err, "isd", isd)
		}

		parts := make(map[string]cppki.SignedTRC, len(voterList)*2)
		for _, voter := range voterList {
			voterInfo, err := loadVoterInfo(voter, cryptoVotingDir(voter, cfg.out))
			if err != nil {
				return err
			}

			sensitive, err := signPayload(raw, voterInfo.sensitiveKey, voterInfo.sensitiveCert)
			if err != nil {
				return serrors.Wrap("signing TRC payload - sensitive", err)
			}
			parts[fmt.Sprintf("ISD%d-B1-S1.%s-sensitive.trc", isd, voter)] = sensitive
			regular, err := signPayload(raw, voterInfo.regularKey, voterInfo.regularCert)
			if err != nil {
				return serrors.Wrap("signing TRC payload - regular", err)
			}
			parts[fmt.Sprintf("ISD%d-B1-S1.%s-regular.trc", isd, voter)] = regular
		}

		combined, err := trcs.CombineSignedPayloads(parts)
		if err != nil {
			return serrors.Wrap("combining signed TRC payloads", err)
		}
		combined = pem.EncodeToMemory(&pem.Block{
			Type:  "TRC",
			Bytes: combined,
		})
		if err := os.WriteFile(filepath.Join(trcDir(isd, cfg.out),
			fmt.Sprintf("ISD%d-B1-S1.trc", isd)), combined, 0644); err != nil {
			return serrors.Wrap("writing TRC", err)
		}
	}
	return nil
}

func loadVoterInfo(voter addr.IA, votingDir string) (*voterInfo, error) {
	// Prefer per-IA key filenames, fallback to legacy generic names for compatibility
	sensitiveKeyPath := filepath.Join(votingDir, sensitiveKeyName(voter))
	if _, err := os.Stat(sensitiveKeyPath); errors.Is(err, os.ErrNotExist) {
		sensitiveKeyPath = filepath.Join(votingDir, "sensitive-voting.key")
	}
	sensitiveKey, err := key.LoadPrivateKey("", sensitiveKeyPath)
	if err != nil {
		return nil, serrors.Wrap("loading sensitive key", err)
	}
	regularKeyPath := filepath.Join(votingDir, regularKeyName(voter))
	if _, err := os.Stat(regularKeyPath); errors.Is(err, os.ErrNotExist) {
		regularKeyPath = filepath.Join(votingDir, "regular-voting.key")
	}
	regularKey, err := key.LoadPrivateKey("", regularKeyPath)
	if err != nil {
		return nil, serrors.Wrap("loading regular key", err)
	}
	sensitiveCerts, err := cppki.ReadPEMCerts(
		filepath.Join(votingDir, sensitiveCertName(voter)))
	if err != nil {
		return nil, serrors.Wrap("loading sensitive cert", err)
	}
	if len(sensitiveCerts) > 1 {
		return nil, serrors.New("more than one sensitive cert found", "ia", voter)
	}
	regularCerts, err := cppki.ReadPEMCerts(
		filepath.Join(votingDir, regularCertName(voter)))
	if err != nil {
		return nil, serrors.Wrap("loading regular cert", err)
	}
	if len(regularCerts) > 1 {
		return nil, serrors.New("more than one regular cert found", "ia", voter)
	}

	return &voterInfo{
		sensitiveKey:  sensitiveKey,
		regularKey:    regularKey,
		sensitiveCert: sensitiveCerts[0],
		regularCert:   regularCerts[0],
	}, nil
}

func signPayload(pld []byte, key crypto.Signer, cert *x509.Certificate) (cppki.SignedTRC, error) {
	signedPld, err := trcs.SignPayload(pld, key, cert)
	if err != nil {
		return cppki.SignedTRC{}, err
	}
	return cppki.DecodeSignedTRC(signedPld)
}

func sortASSet(set map[addr.AS]struct{}) []addr.AS {
	res := make([]addr.AS, 0, len(set))
	for as := range set {
		res = append(res, as)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	return res
}

func sortIASet(set map[addr.IA]struct{}) []addr.IA {
	res := make([]addr.IA, 0, len(set))
	for ia := range set {
		res = append(res, ia)
	}
	sort.Slice(res, func(i, j int) bool { return uint64(res[i]) < uint64(res[j]) })
	return res
}

func sortStringSet(set map[string]struct{}) []string {
	res := make([]string, 0, len(set))
	for s := range set {
		res = append(res, s)
	}
	sort.Strings(res)
	return res
}

func setupTemplates(cfg config) error {
	for ia, d := range cfg.topo.ASes {
		baseAttrs := cfg.topo.baseAttrs(ia)
		for _, view := range membershipViews(ia, baseAttrs, d.PrivateISDs, d.CA) {
			entries := map[string]certs.SubjectVars{
				cpASTemplatePath(view.IA, cfg.out): {
					IA:         view.IA,
					CommonName: view.IA.String() + " AS Certificate",
				},
			}
			if view.Issuing {
				entries[cpRootTemplatePath(view.IA, cfg.out)] = certs.SubjectVars{
					IA:         view.IA,
					CommonName: view.IA.String() + " Root Certificate - GEN I",
				}
				entries[cpCATemplatePath(view.IA, cfg.out)] = certs.SubjectVars{
					IA:         view.IA,
					CommonName: fmt.Sprintf("%s CA Certificate - GEN I %d.1", view.IA, time.Now().Year()),
				}
			}
			if view.Voting {
				entries[regularVotingTemplatePath(view.IA, cfg.out)] = certs.SubjectVars{
					IA:         view.IA,
					CommonName: view.IA.String() + " Regular Voting Certificate",
				}
				entries[sensitiveVotingTemplatePath(view.IA, cfg.out)] = certs.SubjectVars{
					IA:         view.IA,
					CommonName: view.IA.String() + " Sensitive Voting Certificate",
				}
			}
			for fn, tmpl := range entries {
				if err := os.MkdirAll(filepath.Dir(fn), 0o755); err != nil {
					return err
				}
				file, err := os.Create(fn)
				if err != nil {
					return err
				}
				enc := json.NewEncoder(file)
				enc.SetIndent("", "    ")
				if err := enc.Encode(tmpl); err != nil {
					file.Close()
					return err
				}
				if err := file.Close(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func prepareDirectories(t topo, out outConfig) error {
	for ia, d := range t.ASes {
		baseAttrs := t.baseAttrs(ia)
		for _, view := range membershipViews(ia, baseAttrs, d.PrivateISDs, d.CA) {
			dirs := []string{
				trcDir(view.IA.ISD(), out),
				keyDir(view.IA, out),
				certDir(view.IA, out),
				cryptoASDir(view.IA, out),
				filepath.Join(out.base, "trcs"),
				filepath.Join(out.base, "certs"),
			}
			if view.Issuing {
				dirs = append(dirs, cryptoCADir(view.IA, out))
			}
			if view.Voting {
				dirs = append(dirs, cryptoVotingDir(view.IA, out))
			}
			for _, dir := range dirs {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func flatten(out outConfig) error {
	trcs, err := filepath.Glob(fmt.Sprintf("%s/ISD*/trcs/ISD*-B*-S*.trc", out.base))
	if err != nil {
		return err
	}
	for _, trc := range trcs {
		_, name := filepath.Split(trc)
		if err := copyFile(filepath.Join(out.base, "trcs", name), trc); err != nil {
			return serrors.Wrap("copying", err, "file", trc)
		}
	}

	prefix := filepath.Join(out.base, "AS*")
	if out.isd {
		prefix = filepath.Join(out.base, "ISD*", "AS*")
	}

	pems, err := filepath.Glob(fmt.Sprintf("%s/crypto/*/ISD*-AS*.pem", prefix))
	if err != nil {
		return err
	}
	crts, err := filepath.Glob(fmt.Sprintf("%s/crypto/*/ISD*-AS*.crt", prefix))
	if err != nil {
		return err
	}
	for _, file := range append(pems, crts...) {
		_, name := filepath.Split(file)
		if err := copyFile(filepath.Join(out.base, "certs", name), file); err != nil {
			return serrors.Wrap("copying", err, "file", file)
		}
	}
	return nil
}

func cleanup(cfg config) error {
	base := cfg.out.base
	if cfg.out.isd {
		base = filepath.Join(base, "*/")
	}
	var files []string
	match, err := filepath.Glob(filepath.Join(base, "*/crypto/*/cp-*.crt"))
	if err != nil {
		return err
	}
	files = append(files, match...)
	match, err = filepath.Glob(filepath.Join(base, "*/crypto/*/regular-*.crt"))
	if err != nil {
		return err
	}
	files = append(files, match...)
	match, err = filepath.Glob(filepath.Join(base, "*/crypto/*/sensitive-*.crt"))
	if err != nil {
		return err
	}
	files = append(files, match...)
	match, err = filepath.Glob(filepath.Join(base, "*/crypto/voting/ISD*-B1-S1.*.trc"))
	if err != nil {
		return err
	}
	files = append(files, match...)
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(dst string, src string) error {
	if dst == src {
		return nil
	}
	dfile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dfile.Close()
	sfile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sfile.Close()
	if _, err := io.Copy(dfile, sfile); err != nil {
		return err
	}
	return nil
}

type outConfig struct {
	base string
	isd  bool
}

func (cfg outConfig) AS(ia addr.IA) string {
	if cfg.isd {
		return filepath.Join(
			cfg.base,
			addr.FormatISD(ia.ISD(), addr.WithDefaultPrefix()),
			addr.FormatAS(ia.AS(), addr.WithDefaultPrefix(), addr.WithFileSeparator()),
		)
	}
	return filepath.Join(
		cfg.base,
		addr.FormatAS(ia.AS(), addr.WithDefaultPrefix(), addr.WithFileSeparator()),
	)
}

func trcDir(isd addr.ISD, out outConfig) string {
	return filepath.Join(out.base, addr.FormatISD(isd, addr.WithDefaultPrefix()), "trcs")
}

func keyDir(ia addr.IA, out outConfig) string {
	return filepath.Join(out.AS(ia), "keys")
}

func certDir(ia addr.IA, out outConfig) string {
	return filepath.Join(out.AS(ia), "certs")
}

func cryptoASDir(ia addr.IA, out outConfig) string {
	return filepath.Join(out.AS(ia), "crypto", "as")
}

func cryptoCADir(ia addr.IA, out outConfig) string {
	return filepath.Join(out.AS(ia), "crypto", "ca")
}

func cryptoVotingDir(ia addr.IA, out outConfig) string {
	return filepath.Join(out.AS(ia), "crypto", "voting")
}

func cpASTemplatePath(ia addr.IA, out outConfig) string {
	return filepath.Join(cryptoASDir(ia, out), fmt.Sprintf("cp-as.%s.tmpl", fmtIA(ia)))
}

func cpRootTemplatePath(ia addr.IA, out outConfig) string {
	return filepath.Join(cryptoCADir(ia, out), fmt.Sprintf("cp-root.%s.tmpl", fmtIA(ia)))
}

func cpCATemplatePath(ia addr.IA, out outConfig) string {
	return filepath.Join(cryptoCADir(ia, out), fmt.Sprintf("cp-ca.%s.tmpl", fmtIA(ia)))
}

func regularVotingTemplatePath(ia addr.IA, out outConfig) string {
	return filepath.Join(cryptoVotingDir(ia, out), fmt.Sprintf("regular.%s.tmpl", fmtIA(ia)))
}

func sensitiveVotingTemplatePath(ia addr.IA, out outConfig) string {
	return filepath.Join(cryptoVotingDir(ia, out), fmt.Sprintf("sensitive.%s.tmpl", fmtIA(ia)))
}

func chainName(ia addr.IA) string {
	return fmt.Sprintf("%s.pem", fmtIA(ia))
}

func caCertName(ia addr.IA) string {
	return fmt.Sprintf("%s.ca.crt", fmtIA(ia))
}

func rootCertName(ia addr.IA, serial ...int) string {
	if len(serial) == 0 {
		return fmt.Sprintf("%s.root.crt", fmtIA(ia))
	}
	return fmt.Sprintf("%s.root.s%d.crt", fmtIA(ia), serial[0])
}

func sensitiveCertName(ia addr.IA, serial ...int) string {
	if len(serial) == 0 {
		return fmt.Sprintf("%s.sensitive.crt", fmtIA(ia))
	}
	return fmt.Sprintf("%s.sensitive.s%d.crt", fmtIA(ia), serial[0])
}

func regularCertName(ia addr.IA, serial ...int) string {
	if len(serial) == 0 {
		return fmt.Sprintf("%s.regular.crt", fmtIA(ia))
	}
	return fmt.Sprintf("%s.regular.s%d.crt", fmtIA(ia), serial[0])
}

func fmtIA(ia addr.IA) string {
	return addr.FormatIA(ia, addr.WithFileSeparator(), addr.WithDefaultPrefix())
}

// Key naming helpers with per-membership IA disambiguation
func sensitiveKeyName(ia addr.IA) string {
	return fmt.Sprintf("%s.sensitive.key", fmtIA(ia))
}

func regularKeyName(ia addr.IA) string {
	return fmt.Sprintf("%s.regular.key", fmtIA(ia))
}

func cpRootKeyName(ia addr.IA) string {
	return fmt.Sprintf("%s.cp-root.key", fmtIA(ia))
}

func cpCAKeyName(ia addr.IA) string {
	return fmt.Sprintf("%s.cp-ca.key", fmtIA(ia))
}

// returns the path to the CA private key, preferring per-IA named key if present
func preferCAKey(caDir string, ca addr.IA) string {
	named := filepath.Join(caDir, cpCAKeyName(ca))
	if _, err := os.Stat(named); err == nil {
		return named
	}
	return filepath.Join(caDir, "cp-ca.key")
}
