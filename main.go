package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sagernet/sing-box/common/geosite"
	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"

	"github.com/google/go-github/v45/github"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

var githubClient *github.Client

func init() {
	accessToken, loaded := os.LookupEnv("ACCESS_TOKEN")
	if !loaded {
		githubClient = github.NewClient(nil)
		return
	}
	transport := &github.BasicAuthTransport{
		Username: accessToken,
	}
	githubClient = github.NewClient(transport.Client())
}

// downloadFile fetches one file from a repository branch via the GitHub API.
func downloadFile(owner, repo, ref, filePath string) ([]byte, error) {
	log.Info("download ", owner, "/", repo, "@", ref, "/", filePath)
	reader, response, err := githubClient.Repositories.DownloadContents(
		context.Background(), owner, repo, filePath,
		&github.RepositoryContentGetOptions{Ref: ref},
	)
	if err != nil {
		return nil, err
	}
	if response != nil && response.StatusCode != http.StatusOK {
		reader.Close()
		return nil, E.New("failed to download ", filePath, ": status ", response.StatusCode)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// download fetches dlc.dat and its checksum from the source repository's release
// branch, verifies the sha256, and returns the data.
func download(source string) ([]byte, error) {
	names := strings.SplitN(source, "/", 2)
	if len(names) != 2 {
		return nil, E.New("invalid source repository: ", source)
	}
	owner, repo := names[0], names[1]
	data, err := downloadFile(owner, repo, "release", "dlc.dat")
	if err != nil {
		return nil, err
	}
	remoteChecksum, err := downloadFile(owner, repo, "release", "dlc.dat.sha256sum")
	if err != nil {
		return nil, err
	}
	checksum := sha256.Sum256(data)
	if hex.EncodeToString(checksum[:]) != string(remoteChecksum[:64]) {
		return nil, E.New("checksum mismatch")
	}
	return data, nil
}

const syncStatePath = "sync-state.json"

// tagTypeName maps a protobuf domain type to the same stable label used by
// the domain-list-community fingerprint tool, so both repositories compute
// identical fingerprints for the same tag.
func tagTypeName(t routercommon.Domain_Type) string {
	switch t {
	case routercommon.Domain_Plain:
		return "keyword"
	case routercommon.Domain_Regex:
		return "regex"
	case routercommon.Domain_RootDomain:
		return "domain"
	case routercommon.Domain_Full:
		return "full"
	default:
		return "typeX"
	}
}

func splitTagCode(tag string) (string, string, bool) {
	i := strings.Index(tag, "@")
	if i <= 0 || i == len(tag)-1 {
		return "", "", false
	}
	return tag[:i], tag[i+1:], true
}

func domainHasAttr(d *routercommon.Domain, attr string) bool {
	for _, a := range d.Attribute {
		if a.Key == attr {
			return true
		}
	}
	return false
}

// tagFingerprint returns a stable sha256 over the raw (type,value) domains of
// one tag (code@attr) in the source dlc.dat, matching
// domain-list-community's `sync hashtag` output exactly.
func tagFingerprint(vGeositeData []byte, tag string) (string, error) {
	list := &routercommon.GeoSiteList{}
	if err := proto.Unmarshal(vGeositeData, list); err != nil {
		return "", err
	}
	code, attr, ok := splitTagCode(tag)
	if !ok {
		return "", E.New("invalid tag: ", tag)
	}
	var entry *routercommon.GeoSite
	for _, e := range list.Entry {
		if strings.EqualFold(e.CountryCode, code) {
			entry = e
			break
		}
	}
	if entry == nil {
		return "", E.New("entry not found in upstream data: ", code)
	}
	lines := make([]string, 0, len(entry.Domain))
	for _, d := range entry.Domain {
		if domainHasAttr(d, attr) {
			lines = append(lines, tagTypeName(d.Type)+"\x00"+d.Value)
		}
	}
	sort.Strings(lines)
	unique := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 || line != lines[i-1] {
			unique = append(unique, line)
		}
	}
	h := sha256.New()
	for _, line := range unique {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func loadSyncState() map[string]string {
	state := map[string]string{}
	content, err := os.ReadFile(syncStatePath)
	if err != nil {
		return state
	}
	if err = json.Unmarshal(content, &state); err != nil {
		return map[string]string{}
	}
	return state
}

func saveSyncState(state map[string]string) error {
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(syncStatePath, append(content, '\n'), 0o644)
}

// tagsUnchanged reports whether every target tag fingerprint in the upstream
// data equals the last built fingerprint recorded in sync-state.json.
func tagsUnchanged(vGeositeData []byte) (bool, error) {
	previous := loadSyncState()
	if len(previous) == 0 {
		return false, nil
	}
	for _, tag := range targetTags {
		current, err := tagFingerprint(vGeositeData, tag)
		if err != nil {
			return false, err
		}
		if previous[tag] != current {
			return false, nil
		}
	}
	return true, nil
}

// parseTag extracts the domains of one tag (code@attr) from the source
// dlc.dat and converts them to sing-box geosite items. It returns loaded=false
// when the entry is absent from the upstream data.
func parseTag(vGeositeData []byte, tag string) (domains []geosite.Item, loaded bool, err error) {
	list := &routercommon.GeoSiteList{}
	if err = proto.Unmarshal(vGeositeData, list); err != nil {
		return nil, false, err
	}
	code, attr, ok := splitTagCode(tag)
	if !ok {
		return nil, false, E.New("invalid tag: ", tag)
	}
	var entry *routercommon.GeoSite
	for _, e := range list.Entry {
		if strings.EqualFold(e.CountryCode, code) {
			entry = e
			break
		}
	}
	if entry == nil {
		return nil, false, nil
	}
	domains = make([]geosite.Item, 0, len(entry.Domain)*2)
	for _, domain := range entry.Domain {
		if !domainHasAttr(domain, attr) {
			continue
		}
		switch domain.Type {
		case routercommon.Domain_Plain:
			domains = append(domains, geosite.Item{
				Type:  geosite.RuleTypeDomainKeyword,
				Value: domain.Value,
			})
		case routercommon.Domain_Regex:
			domains = append(domains, geosite.Item{
				Type:  geosite.RuleTypeDomainRegex,
				Value: domain.Value,
			})
		case routercommon.Domain_RootDomain:
			if strings.Contains(domain.Value, ".") {
				domains = append(domains, geosite.Item{
					Type:  geosite.RuleTypeDomain,
					Value: domain.Value,
				})
			}
			domains = append(domains, geosite.Item{
				Type:  geosite.RuleTypeDomainSuffix,
				Value: "." + domain.Value,
			})
		case routercommon.Domain_Full:
			domains = append(domains, geosite.Item{
				Type:  geosite.RuleTypeDomain,
				Value: domain.Value,
			})
		}
	}
	return common.Uniq(domains), true, nil
}

var targetTags = []string{
	"cn@!cn",
	"geolocation-!cn@cn",
}

func generate(vData []byte, ruleSetOutput string) error {
	if os.Getenv("NO_SKIP") != "true" {
		unchanged, err := tagsUnchanged(vData)
		if err != nil {
			return err
		}
		if unchanged {
			log.Info("upstream cn@!cn and geolocation-!cn@cn unchanged, skip build")
			setActionOutput("skip", "true")
			return nil
		}
	}
	os.RemoveAll(ruleSetOutput)
	if err := os.MkdirAll(ruleSetOutput, 0o755); err != nil {
		return err
	}
	for _, code := range targetTags {
		domains, loaded, err := parseTag(vData, code)
		if err != nil {
			return err
		}
		if !loaded {
			log.Warn("tag not found in upstream data: ", code)
			continue
		}
		var headlessRule option.DefaultHeadlessRule
		defaultRule := geosite.Compile(domains)
		headlessRule.Domain = defaultRule.Domain
		headlessRule.DomainSuffix = defaultRule.DomainSuffix
		headlessRule.DomainKeyword = defaultRule.DomainKeyword
		headlessRule.DomainRegex = defaultRule.DomainRegex
		var plainRuleSet option.PlainRuleSet
		plainRuleSet.Rules = []option.HeadlessRule{
			{
				Type:           C.RuleTypeDefault,
				DefaultOptions: headlessRule,
			},
		}
		jsonPath, _ := filepath.Abs(filepath.Join(ruleSetOutput, code+".json"))
		ruleSetCompat := option.PlainRuleSetCompat{
			Version: C.RuleSetVersionCurrent,
			Options: plainRuleSet,
		}
		jsonContent, err := json.MarshalIndent(ruleSetCompat, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(jsonPath, append(jsonContent, '\n'), 0o644); err != nil {
			return err
		}
		srsPath, _ := filepath.Abs(filepath.Join(ruleSetOutput, code+".srs"))
		outputRuleSet, err := os.Create(srsPath)
		if err != nil {
			return err
		}
		err = srs.Write(outputRuleSet, plainRuleSet, C.RuleSetVersionCurrent)
		if err != nil {
			outputRuleSet.Close()
			return err
		}
		outputRuleSet.Close()
		log.Info("generated ", code, " (", len(domains), " items)")
	}
	state := make(map[string]string, len(targetTags))
	for _, code := range targetTags {
		fingerprint, err := tagFingerprint(vData, code)
		if err != nil {
			return err
		}
		state[code] = fingerprint
	}
	if err := saveSyncState(state); err != nil {
		return err
	}
	return nil
}

func setActionOutput(name string, content string) {
	ghOutput, ok := os.LookupEnv("GITHUB_OUTPUT")
	if ok {
		f, err := os.OpenFile(ghOutput, os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			f.WriteString(name + "=" + content + "\n")
			f.Close()
		}
	}
}

func release(source string, ruleSetOutput string) error {
	vData, err := download(source)
	if err != nil {
		return err
	}
	return generate(vData, ruleSetOutput)
}

func main() {
	err := release(
		"yagh779/domain-list-community",
		"rule-set",
	)
	if err != nil {
		log.Fatal(err)
	}
}
