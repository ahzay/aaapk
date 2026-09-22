package source

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/ahzay/aaapk/pkg/source/gplay/proto"
	"google.golang.org/protobuf/proto"
)

const (
	gpBaseURL    = "https://android.clients.google.com"
	gpFdfeURL    = gpBaseURL + "/fdfe"
	gpDetailsURL = gpFdfeURL + "/details"
	gpSearchURL  = gpFdfeURL + "/search"
	gpPurchURL   = gpFdfeURL + "/purchase"
	gpDelivURL   = gpFdfeURL + "/delivery"
)

type GPlay struct {
	name         string
	dispenserURL string

	email, authToken, authSubToken string
	gsfID                          string
	checkinToken                   string
	configToken                    string
	dfeCookie                      string

	ready bool
}

func NewGPlay(name, dispenserURL string) *GPlay {
	return &GPlay{
		name:         name,
		dispenserURL: dispenserURL,
	}
}

func (g *GPlay) Name() string { return g.name }

func (g *GPlay) init() error {
	if g.ready {
		return nil
	}
	if err := g.dispenserAuth(); err != nil {
		return fmt.Errorf("gplay %s dispenser: %w", g.name, err)
	}
	g.ready = true
	return nil
}

func (g *GPlay) dispenserAuth() error {
	body, err := json.Marshal(gplayDeviceProps)
	if err != nil {
		return fmt.Errorf("marshal props: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("POST", g.dispenserURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "com.aurora.store-4.8.1-73")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Connection", "keep-alive")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("request: %w", err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode == 200 {
			var bundle struct {
				Email        string `json:"email"`
				AuthToken    string `json:"authToken"`
				GSFID        string `json:"gsfId"`
				CheckinToken string `json:"deviceCheckInConsistencyToken"`
				ConfigToken  string `json:"deviceConfigToken"`
				DFECookie    string `json:"dfeCookie"`
			}
			if err := json.Unmarshal(raw, &bundle); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			if bundle.GSFID == "" {
				return fmt.Errorf("empty gsfId from dispenser")
			}
			if bundle.AuthToken == "" {
				return fmt.Errorf("empty authToken from dispenser")
			}
			g.email = bundle.Email
			g.authToken = bundle.AuthToken
			g.authSubToken = bundle.AuthToken
			g.gsfID = bundle.GSFID
			g.checkinToken = bundle.CheckinToken
			g.configToken = bundle.ConfigToken
			g.dfeCookie = bundle.DFECookie
			return nil
		}

		lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, trunc(string(raw), 200))
		if resp.StatusCode != 429 && resp.StatusCode < 500 {
			break
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return lastErr
}

func (g *GPlay) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+g.authSubToken)
	req.Header.Set("User-Agent", gplayUserAgent())
	req.Header.Set("X-DFE-Device-Id", g.gsfID)
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("X-DFE-Client-Id", "am-android-google")
	req.Header.Set("X-DFE-Network-Type", "4")
	req.Header.Set("X-DFE-Content-Filters", "")
	req.Header.Set("X-Limit-Ad-Tracking-Enabled", "false")
	req.Header.Set("X-Ad-Id", "")
	req.Header.Set("X-DFE-UserLanguages", "en_US")
	req.Header.Set("X-DFE-Request-Params", "timeoutMs=4000")
	req.Header.Set("X-DFE-Encoded-Targets", gplayEncodedTargets)
	req.Header.Set("X-DFE-Phenotype", gplayPhenotype)
	if g.checkinToken != "" {
		req.Header.Set("X-DFE-Device-Checkin-Consistency-Token", g.checkinToken)
	}
	if g.configToken != "" {
		req.Header.Set("X-DFE-Device-Config-Token", g.configToken)
	}
	if g.dfeCookie != "" {
		req.Header.Set("X-DFE-Cookie", g.dfeCookie)
	}
}

func (g *GPlay) fdfeGet(u string) (*pb.ResponseWrapper, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("new request %s: %w", u, err)
	}
	g.setHeaders(req)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", u, err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get %s: http %d: %s", u, resp.StatusCode, trunc(string(body), 200))
	}
	var wrapper pb.ResponseWrapper
	if err := proto.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", u, err)
	}
	return &wrapper, nil
}

func (g *GPlay) fdfePost(u string) (*pb.ResponseWrapper, error) {
	req, err := http.NewRequest("POST", u, nil)
	if err != nil {
		return nil, fmt.Errorf("new request %s: %w", u, err)
	}
	g.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", u, err)
	}
	var wrapper pb.ResponseWrapper
	if err := proto.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", u, err)
	}
	return &wrapper, nil
}

func (g *GPlay) Search(query string) ([]App, error) {
	if err := g.init(); err != nil {
		return nil, fmt.Errorf("gplay %s search init: %w", g.name, err)
	}

	// exact package name -> details lookup
	if strings.Contains(query, ".") && !strings.Contains(query, " ") {
		a, err := g.Resolve(query)
		if err != nil {
			return nil, err
		}
		if a != nil {
			return []App{*a}, nil
		}
	}

	u := gpSearchURL + "?c=3&q=" + url.QueryEscape(query) + "&ksm=1"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	g.setHeaders(req)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}

	var wrapper pb.ResponseWrapper
	if err := proto.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal search: %w", err)
	}

	var payload *pb.Payload
	if wrapper.PreFetch != nil && wrapper.PreFetch.Response != nil && wrapper.PreFetch.Response.Payload != nil {
		payload = wrapper.PreFetch.Response.Payload
	} else if wrapper.Payload != nil {
		payload = wrapper.Payload
	}
	if payload == nil || payload.ListResponse == nil || payload.ListResponse.Item == nil {
		return nil, nil
	}

	var apps []App
	for _, cluster := range payload.ListResponse.Item.SubItem {
		for _, item := range cluster.SubItem {
			if item.GetType() == 1 {
				if a, ok := itemToApp(item, g.name); ok {
					apps = append(apps, a)
				}
			}
		}
	}
	return apps, nil
}

func (g *GPlay) Resolve(pkg string) (*App, error) {
	if err := g.init(); err != nil {
		return nil, fmt.Errorf("gplay %s resolve init: %w", g.name, err)
	}

	u := gpDetailsURL + "?doc=" + url.QueryEscape(pkg)
	wrapper, err := g.fdfeGet(u)
	if err != nil {
		return nil, fmt.Errorf("gplay %s resolve %s: %w", g.name, pkg, err)
	}
	if wrapper.Payload == nil || wrapper.Payload.DetailsResponse == nil || wrapper.Payload.DetailsResponse.Item == nil {
		return nil, nil
	}
	a, ok := itemToApp(wrapper.Payload.DetailsResponse.Item, g.name)
	if !ok {
		return nil, nil
	}
	return &a, nil
}

func (g *GPlay) Download(app App, dest string) (string, error) {
	if err := g.init(); err != nil {
		return "", fmt.Errorf("gplay %s download init: %w", g.name, err)
	}

	// purchase
	purchU := fmt.Sprintf("%s?doc=%s&vc=%d&ot=1", gpPurchURL, url.QueryEscape(app.PackageName), app.VersionCode)
	purchResp, err := g.fdfePost(purchU)
	if err != nil {
		return "", fmt.Errorf("purchase %s: %w", app.PackageName, err)
	}
	var dlToken string
	if purchResp.Payload != nil && purchResp.Payload.BuyResponse != nil {
		if t := purchResp.Payload.BuyResponse.EncodedDeliveryToken; t != nil {
			dlToken = *t
		}
	}

	// delivery
	delivU := fmt.Sprintf("%s?doc=%s&vc=%d&ot=1", gpDelivURL, url.QueryEscape(app.PackageName), app.VersionCode)
	if dlToken != "" {
		delivU += "&dtok=" + url.QueryEscape(dlToken)
	}
	delivResp, err := g.fdfeGet(delivU)
	if err != nil {
		return "", fmt.Errorf("delivery %s: %w", app.PackageName, err)
	}
	if delivResp.Payload == nil || delivResp.Payload.DeliveryResponse == nil || delivResp.Payload.DeliveryResponse.AppDeliveryData == nil {
		return "", fmt.Errorf("no delivery data for %s", app.PackageName)
	}

	dd := delivResp.Payload.DeliveryResponse.AppDeliveryData
	dlURL := dd.GetDownloadUrl()
	if dlURL == "" {
		return "", fmt.Errorf("no download url for %s", app.PackageName)
	}

	apkDir := filepath.Join(dest, fmt.Sprintf("%s-%s", app.PackageName, app.Version))
	if err := os.MkdirAll(apkDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", apkDir, err)
	}

	// base apk
	basePath := filepath.Join(apkDir, "base.apk")
	if err := g.downloadFile(dlURL, dd.DownloadAuthCookie, basePath); err != nil {
		return "", fmt.Errorf("download base %s: %w", app.PackageName, err)
	}

	// split apks
	for _, split := range dd.SplitDeliveryData {
		splitURL := split.GetDownloadUrl()
		if splitURL == "" {
			continue
		}
		splitName := split.GetName() + ".apk"
		splitPath := filepath.Join(apkDir, splitName)
		if err := g.downloadFile(splitURL, nil, splitPath); err != nil {
			return "", fmt.Errorf("download split %s/%s: %w", app.PackageName, splitName, err)
		}
	}

	return apkDir, nil
}

func (g *GPlay) downloadFile(dlURL string, cookies []*pb.HttpCookie, dest string) error {
	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	g.setHeaders(req)
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.GetName(), Value: c.GetValue()})
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

func (g *GPlay) Refresh() error {
	g.ready = false
	return g.init()
}

func itemToApp(item *pb.Item, source string) (App, bool) {
	if item.Details == nil || item.Details.AppDetails == nil {
		return App{}, false
	}
	ad := item.Details.AppDetails
	pkg := ad.GetPackageName()
	if pkg == "" {
		return App{}, false
	}
	return App{
		PackageName: pkg,
		Name:        item.GetTitle(),
		Version:     ad.GetVersionString(),
		VersionCode: int(ad.GetVersionCode()),
		Summary:     item.GetCreator(),
		Source:      source,
		Size:        ad.GetInfoDownloadSize(),
	}, true
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func gplayUserAgent() string {
	p := gplayDeviceProps
	return fmt.Sprintf(
		"Android-Finsky/%s (api=3,versionCode=%s,sdk=%s,device=%s,hardware=%s,product=%s,platformVersionRelease=%s,model=%s,buildId=%s,isWideScreen=0,supportedAbis=%s)",
		p["Vending.versionString"], p["Vending.version"], p["Build.VERSION.SDK_INT"],
		p["Build.DEVICE"], p["Build.HARDWARE"], p["Build.PRODUCT"],
		p["Build.VERSION.RELEASE"], p["Build.MODEL"], p["Build.ID"], p["Platforms"],
	)
}

const gplayEncodedTargets = "CAESN/qigQYC2AMBFfUbyA7SM5Ij/CvfBoIDgxHqGP8R3xzIBvoQtBKFDZ4HAY4FrwSVMasHBO0O2Q8akgYRAQECAQO7AQEpKZ0CnwECAwRrAQYBr9PPAoK7sQMBAQMCBAkIDAgBAwEDBAICBAUZEgMEBAMLAQEBBQEBAcYBARYED+cBfS8CHQEKkAEMMxcBIQoUDwYHIjd3DQ4MFk0JWGYZEREYAQOLAYEBFDMIEYMBAgICAgICOxkCD18LGQKEAcgDBIQBAgGLARkYCy8oBTJlBCUocxQn0QUBDkkGxgNZQq0BZSbeAmIDgAEBOgGtAaMCDAOQAZ4BBIEBKUtQUYYBQscDDxPSARA1oAEHAWmnAsMB2wFyywGLAxol+wImlwOOA80CtwN26A0WjwJVbQEJPAH+BRDeAfkHK/ABASEBCSAaHQemAzkaRiu2Ad8BdXeiAwEBGBUBBN4LEIABK4gB2AFLfwECAdoENq0CkQGMBsIBiQEtiwGgA1zyAUQ4uwS8AwhsvgPyAcEDF27vApsBHaICGhl3GSKxAR8MC6cBAgItmQYG9QIeywLvAeYBDArLAh8HASI4ELICDVmVBgsY/gHWARtcAsMBpALiAdsBA7QBpAJmIArpByn0AyAKBwHTARIHAX8D+AMBcRIBBbEDmwUBMacCHAciNp0BAQF0OgQLJDuSAh54kwFSP0eeAQQ4M5EBQgMEmwFXywFo0gFyWwMcapQBBugBPUW2AVgBKmy3AR6PAbMBGQxrUJECvQR+8gFoWDsYgQNwRSczBRXQAgtRswEW0ALMAREYAUEBIG6yATYCRE8OxgER8gMBvQEDRkwLc8MBTwHZAUOnAXiiBakDIbYBNNcCIUmuArIBSakBrgFHKs0EgwV/G3AD0wE6LgECtQJ4xQFwFbUCjQPkBS6vAQqEAUZF3QIM9wEhCoYCQhXsBCyZArQDugIziALWAdIBlQHwBdUErQE6qQaSA4EEIvYBHir9AQVLmgMCApsCKAwHuwgrENsBAjNYswEVmgIt7QJnN4wDEnta+wGfAcUBxgEtEFXQAQWdAUAeBcwBAQM7rAEJATJ0LENrdh73A6UBhAE+qwEeASxLZUMhDREuH0CGARbd7K0GlQo"
const gplayPhenotype = "H4sIAAAAAAAAAB3OO3KjMAAA0KRNuWXukBkBQkAJ2MhgAZb5u2GCwQZbCH_EJ77QHmgvtDtbv-Z9_H63zXXU0NVPB1odlyGy7751Q3CitlPDvFd8lxhz3tpNmz7P92CFw73zdHU2Ie0Ad2kmR8lxhiErTFLt3RPGfJQHSDy7Clw10bg8kqf2owLokN4SecJTLoSwBnzQSd652_MOf2d1vKBNVedzg4ciPoLz2mQ8efGAgYeLou-l-PXn_7Sna1MfhHuySxt-4esulEDp8Sbq54CPPKjpANW-lkU2IZ0F92LBI-ukCKSptqeq1eXU96LD9nZfhKHdtjSWwJqUm_2r6pMHOxk01saVanmNopjX3YxQafC4iC6T55aRbC8nTI98AF_kItIQAJb5EQxnKTO7TZDWnr01HVPxelb9A2OWX6poidMWl16K54kcu_jhXw-JSBQkVcD_fPsLSZu6joIBAAA"
