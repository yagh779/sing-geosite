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

func fetch(from string) (*github.RepositoryRelease, error) {
	names := strings.SplitN(from, "/", 2)
	latestRelease, _, err := githubClient.Repositories.GetLatestRelease(context.Background(), names[0], names[1])
	if err != nil {
		return nil, err
	}
	return latestRelease, err
}

func get(downloadURL *string) ([]byte, error) {
	log.Info("download ", *downloadURL)
	response, err := http.Get(*downloadURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

func download(release *github.RepositoryRelease) ([]byte, error) {
	geositeAsset := common.Find(release.Assets, func(it *github.ReleaseAsset) bool {
		return *it.Name == "dlc.dat"
	})
	geositeChecksumAsset := common.Find(release.Assets, func(it *github.ReleaseAsset) bool {
		return *it.Name == "dlc.dat.sha256sum"
	})
	if geositeAsset == nil {
		return nil, E.New("geosite asset not found in upstream release ", release.Name)
	}
	if geositeChecksumAsset == nil {
		return nil, E.New("geosite asset not found in upstream release ", release.Name)
	}
	data, err := get(geositeAsset.BrowserDownloadURL)
	if err != nil {
		return nil, err
	}
	remoteChecksum, err := get(geositeChecksumAsset.BrowserDownloadURL)
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

func parse(vGeositeData []byte) (map[string][]geosite.Item, error) {
	vGeositeList := routercommon.GeoSiteList{}
	err := proto.Unmarshal(vGeositeData, &vGeositeList)
	if err != nil {
		return nil, err
	}
	domainMap := make(map[string][]geosite.Item)
	for _, vGeositeEntry := range vGeositeList.Entry {
		code := strings.ToLower(vGeositeEntry.CountryCode)
		domains := make([]geosite.Item, 0, len(vGeositeEntry.Domain)*2)
		attributes := make(map[string][]*routercommon.Domain)
		for _, domain := range vGeositeEntry.Domain {
			if len(domain.Attribute) > 0 {
				for _, attribute := range domain.Attribute {
					attributes[attribute.Key] = append(attributes[attribute.Key], domain)
				}
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
		domainMap[code] = common.Uniq(domains)
		for attribute, attributeEntries := range attributes {
			attributeDomains := make([]geosite.Item, 0, len(attributeEntries)*2)
			for _, domain := range attributeEntries {
				switch domain.Type {
				case routercommon.Domain_Plain:
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomainKeyword,
						Value: domain.Value,
					})
				case routercommon.Domain_Regex:
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomainRegex,
						Value: domain.Value,
					})
				case routercommon.Domain_RootDomain:
					if strings.Contains(domain.Value, ".") {
						attributeDomains = append(attributeDomains, geosite.Item{
							Type:  geosite.RuleTypeDomain,
							Value: domain.Value,
						})
					}
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomainSuffix,
						Value: "." + domain.Value,
					})
				case routercommon.Domain_Full:
					attributeDomains = append(attributeDomains, geosite.Item{
						Type:  geosite.RuleTypeDomain,
						Value: domain.Value,
					})
				}
			}
			domainMap[code+"@"+attribute] = common.Uniq(attributeDomains)
		}
	}
	return domainMap, nil
}

func filterTags(data map[string][]geosite.Item) {
	var codeList []string
	for code := range data {
		codeList = append(codeList, code)
	}

	var filteredCodeMap []string

	for _, code := range codeList {
		codeParts := strings.Split(code, "@")
		if len(codeParts) != 2 {
			continue
		}
		leftParts := strings.Split(codeParts[0], "-")
		var lastName string
		if len(leftParts) > 1 {
			lastName = leftParts[len(leftParts)-1]
		}
		if lastName == "" {
			lastName = codeParts[0]
		}
		if lastName == codeParts[1] {
			delete(data, code)
			filteredCodeMap = append(filteredCodeMap, code)
			continue
		}

	}
	sort.Strings(filteredCodeMap)

	os.Stderr.WriteString("filtered " + strings.Join(filteredCodeMap, ",") + "\n")

}

var targetTags = []string{
	"cn@!cn",
	"geolocation-!cn@cn",
}

func generate(release *github.RepositoryRelease, ruleSetOutput string) error {
	vData, err := download(release)
	if err != nil {
		return err
	}
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
	domainMap, err := parse(vData)
	if err != nil {
		return err
	}
	filterTags(domainMap)
	os.RemoveAll(ruleSetOutput)
	err = os.MkdirAll(ruleSetOutput, 0o755)
	if err != nil {
		return err
	}
	for _, code := range targetTags {
		domains, loaded := domainMap[code]
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
	if err = saveSyncState(state); err != nil {
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
	sourceRelease, err := fetch(source)
	if err != nil {
		return err
	}
	err = generate(sourceRelease, ruleSetOutput)
	if err != nil {
		return err
	}
	setActionOutput("tag", *sourceRelease.Name)
	return nil
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
