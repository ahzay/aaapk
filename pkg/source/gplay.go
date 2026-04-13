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
	"strconv"
	"strings"

	"github.com/ahzay/aaapk/pkg/adb"
	pb "github.com/ahzay/aaapk/pkg/source/gplay/proto"
	"google.golang.org/protobuf/proto"
)

const (
	gpBaseURL    = "https://android.clients.google.com"
	gpCheckinURL = gpBaseURL + "/checkin"
	gpFdfeURL    = gpBaseURL + "/fdfe"
	gpDetailsURL = gpFdfeURL + "/details"
	gpSearchURL  = gpFdfeURL + "/search"
	gpPurchURL   = gpFdfeURL + "/purchase"
	gpDelivURL   = gpFdfeURL + "/delivery"
	gpTocURL     = gpFdfeURL + "/toc"
	gpUploadURL  = gpFdfeURL + "/uploadDeviceConfig"
)

type GPlay struct {
	name         string
	dispenserURL string

	email, authToken, authSubToken string
	gsfID, securityToken           uint64
	checkinToken                   string
	configToken                    string
	dfeCookie                      string

	props map[string]string
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
	if err := g.loadDeviceProps(); err != nil {
		return fmt.Errorf("gplay %s device props: %w", g.name, err)
	}
	if err := g.dispenserAuth(); err != nil {
		return fmt.Errorf("gplay %s dispenser: %w", g.name, err)
	}
	g.authSubToken = g.authToken
	if err := g.checkin(); err != nil {
		return fmt.Errorf("gplay %s checkin: %w", g.name, err)
	}
	if err := g.uploadDeviceConfig(); err != nil {
		return fmt.Errorf("gplay %s upload config: %w", g.name, err)
	}
	if err := g.toc(); err != nil {
		return fmt.Errorf("gplay %s toc: %w", g.name, err)
	}
	g.ready = true
	return nil
}

func (g *GPlay) loadDeviceProps() error {
	if g.props != nil {
		return nil
	}
	gp, err := adb.GetProperties()
	if err != nil {
		return fmt.Errorf("getprop: %w", err)
	}

	w, h := adb.ScreenSize()
	density := adb.ScreenDensity()
	features := adb.Features()
	libraries := adb.Libraries()

	radio := gp["gsm.version.baseband"]
	if radio == "" {
		radio = "unknown"
	}
	abis := gp["ro.product.cpu.abilist"]
	if abis == "" {
		abis = gp["ro.product.cpu.abi"]
	}

	g.props = map[string]string{
		"Build.HARDWARE":        gp["ro.hardware"],
		"Build.RADIO":           radio,
		"Build.FINGERPRINT":     gp["ro.build.fingerprint"],
		"Build.BRAND":           gp["ro.product.brand"],
		"Build.DEVICE":          gp["ro.product.device"],
		"Build.VERSION.SDK_INT": gp["ro.build.version.sdk"],
		"Build.VERSION.RELEASE": gp["ro.build.version.release"],
		"Build.MODEL":           gp["ro.product.model"],
		"Build.MANUFACTURER":    gp["ro.product.manufacturer"],
		"Build.PRODUCT":         gp["ro.product.name"],
		"Build.ID":              gp["ro.build.id"],
		"Build.BOOTLOADER":      gp["ro.bootloader"],
		"UserReadableName":      gp["ro.product.manufacturer"] + " " + gp["ro.product.model"],
		"Screen.Width":          w,
		"Screen.Height":         h,
		"Screen.Density":        density,
		"Platforms":             abis,
		"TouchScreen":           "3",
		"Keyboard":              "1",
		"Navigation":            "1",
		"ScreenLayout":          "2",
		"HasHardKeyboard":       "false",
		"HasFiveWayNavigation":  "false",
		"GL.Version":            "196610",
		"GL.Extensions":         "",
		"Features":              strings.Join(features, ","),
		"SharedLibraries":       strings.Join(libraries, ","),
		"Locales":               "en,en_US",
		"Client":                "android-google",
		"GSF.version":           "223616055",
		"Vending.version":       "82151710",
		"Vending.versionString": "21.5.17-21 [0] [PR] 326734551",
		"Roaming":               "mobile-notroaming",
		"TimeZone":              "UTC-10",
		"CellOperator":          "310",
		"SimOperator":           "38",
	}
	return nil
}

func (g *GPlay) dispenserAuth() error {
	body, err := json.Marshal(g.props)
	if err != nil {
		return fmt.Errorf("marshal props: %w", err)
	}
	req, err := http.NewRequest("POST", g.dispenserURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "com.aurora.store-4.8.1-73")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, trunc(string(b), 200))
	}

	var dr struct {
		Email     string `json:"email"`
		AuthToken string `json:"authToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	g.email = dr.Email
	g.authToken = dr.AuthToken
	return nil
}

func (g *GPlay) checkin() error {
	sdkInt, _ := strconv.Atoi(g.props["Build.VERSION.SDK_INT"])
	gsfVer, _ := strconv.Atoi(g.props["GSF.version"])

	build := &pb.AndroidBuildProto{
		Id:             sp(g.props["Build.FINGERPRINT"]),
		Product:        sp(g.props["Build.HARDWARE"]),
		Carrier:        sp(g.props["Build.BRAND"]),
		Radio:          sp(g.props["Build.RADIO"]),
		Bootloader:     sp(g.props["Build.BOOTLOADER"]),
		Client:         sp(g.props["Client"]),
		Timestamp:      ip64(0),
		GoogleServices: ip32(int32(gsfVer)),
		Device:         sp(g.props["Build.DEVICE"]),
		SdkVersion:     ip32(int32(sdkInt)),
		Model:          sp(g.props["Build.MODEL"]),
		Manufacturer:   sp(g.props["Build.MANUFACTURER"]),
		BuildProduct:   sp(g.props["Build.PRODUCT"]),
		OtaInstalled:   bp(false),
	}

	deviceConfig := g.buildDeviceConfig()

	checkinReq := &pb.AndroidCheckinRequest{
		Checkin: &pb.AndroidCheckinProto{
			Build:        build,
			CellOperator: sp(g.props["CellOperator"]),
			SimOperator:  sp(g.props["SimOperator"]),
			Roaming:      sp(g.props["Roaming"]),
		},
		Locale:              sp("en_US"),
		TimeZone:            sp(g.props["TimeZone"]),
		Version:             ip32(3),
		DeviceConfiguration: deviceConfig,
		Fragment:            ip32(0),
	}

	data, err := proto.Marshal(checkinReq)
	if err != nil {
		return fmt.Errorf("marshal checkin: %w", err)
	}

	resp, err := http.Post(gpCheckinURL, "application/x-protobuf", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("post checkin: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read checkin response: %w", err)
	}

	var checkinResp pb.AndroidCheckinResponse
	if err := proto.Unmarshal(body, &checkinResp); err != nil {
		return fmt.Errorf("unmarshal checkin response: %w", err)
	}

	g.gsfID = checkinResp.GetAndroidId()
	g.securityToken = checkinResp.GetSecurityToken()
	if checkinResp.DeviceCheckinConsistencyToken != nil {
		g.checkinToken = *checkinResp.DeviceCheckinConsistencyToken
	}

	// second pass with account cookies
	checkinReq.Id = ip64(int64(g.gsfID))
	checkinReq.SecurityToken = &g.securityToken
	checkinReq.AccountCookie = []string{"[" + g.email + "]", g.authToken}

	data, err = proto.Marshal(checkinReq)
	if err != nil {
		return fmt.Errorf("marshal checkin2: %w", err)
	}
	resp2, err := http.Post(gpCheckinURL, "application/x-protobuf", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("post checkin2: %w", err)
	}
	resp2.Body.Close()
	return nil
}

func (g *GPlay) uploadDeviceConfig() error {
	upload := &pb.UploadDeviceConfigRequest{
		DeviceConfiguration: g.buildDeviceConfig(),
		Manufacturer:        sp(g.props["Build.MANUFACTURER"]),
	}
	data, err := proto.Marshal(upload)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	req, err := http.NewRequest("POST", gpUploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	g.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var wrapper pb.ResponseWrapper
	if err := proto.Unmarshal(body, &wrapper); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	if wrapper.Payload != nil && wrapper.Payload.UploadDeviceConfigResponse != nil {
		if t := wrapper.Payload.UploadDeviceConfigResponse.UploadDeviceConfigToken; t != nil {
			g.configToken = *t
		}
	}
	return nil
}

func (g *GPlay) toc() error {
	req, err := http.NewRequest("GET", gpTocURL, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	g.setHeaders(req)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var wrapper pb.ResponseWrapper
	if err := proto.Unmarshal(body, &wrapper); err != nil {
		return fmt.Errorf("unmarshal toc: %w", err)
	}
	if wrapper.Payload != nil && wrapper.Payload.TocResponse != nil {
		toc := wrapper.Payload.TocResponse
		if toc.Cookie != nil {
			g.dfeCookie = *toc.Cookie
		}
		if toc.TosToken != nil && toc.TosContent != nil {
			if err := g.acceptTos(*toc.TosToken); err != nil {
				return fmt.Errorf("accept tos: %w", err)
			}
		}
	}
	return nil
}

func (g *GPlay) acceptTos(token string) error {
	u := gpFdfeURL + "/acceptTos?tost=" + url.QueryEscape(token) + "&toscme=false"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	g.setHeaders(req)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (g *GPlay) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+g.authSubToken)
	req.Header.Set("User-Agent", fmt.Sprintf(
		"Android-Finsky/29.2.15-21 [0] [PR] 426536134 (api=3,versionCode=82921510,sdk=%s,device=%s,hardware=%s,product=%s,build=%s:us)",
		g.props["Build.VERSION.SDK_INT"], g.props["Build.DEVICE"],
		g.props["Build.HARDWARE"], g.props["Build.PRODUCT"], g.props["Build.ID"],
	))
	req.Header.Set("X-DFE-Device-Id", fmt.Sprintf("%x", g.gsfID))
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
	g.props = nil
	return g.init()
}

func (g *GPlay) buildDeviceConfig() *pb.DeviceConfigurationProto {
	atoi := func(k string) int32 { v, _ := strconv.Atoi(g.props[k]); return int32(v) }

	cfg := &pb.DeviceConfigurationProto{
		TouchScreen:          ip32(atoi("TouchScreen")),
		Keyboard:             ip32(atoi("Keyboard")),
		Navigation:           ip32(atoi("Navigation")),
		ScreenLayout:         ip32(atoi("ScreenLayout")),
		ScreenDensity:        ip32(atoi("Screen.Density")),
		GlEsVersion:          ip32(atoi("GL.Version")),
		ScreenWidth:          ip32(atoi("Screen.Width")),
		ScreenHeight:         ip32(atoi("Screen.Height")),
		HasHardKeyboard:      bp(g.props["HasHardKeyboard"] == "true"),
		HasFiveWayNavigation: bp(g.props["HasFiveWayNavigation"] == "true"),
	}

	split := func(k string) []string {
		if v := g.props[k]; v != "" {
			return strings.Split(v, ",")
		}
		return nil
	}
	cfg.NativePlatform = split("Platforms")
	cfg.SystemAvailableFeature = split("Features")
	cfg.SystemSharedLibrary = split("SharedLibraries")
	cfg.SystemSupportedLocale = split("Locales")
	cfg.GlExtension = split("GL.Extensions")

	return cfg
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

func sp(s string) *string { return &s }
func ip32(i int32) *int32 { return &i }
func ip64(i int64) *int64 { return &i }
func bp(b bool) *bool     { return &b }

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

const gplayEncodedTargets = "CAESN/qigQYC2AMBFfUbyA7SM5Ij/CvfBoIDgxHqGP8R3xzIBvoQtBKFDZ4HAY4FrwSVMasHBO0O2Q8akgYRAQECAQO7AQEpKZ0CnwECAwRrAQYBr9PPAoK7sQMBAQMCBAkIDAgBAwEDBAICBAUZEgMEBAMLAQEBBQEBAcYBARYED+cBfS8CHQEKkAEMMxcBIQoUDwYHIjd3DQ4MFk0JWGYZEREYAQOLAYEBFDMIEYMBAgICAgICOxkCD18LGQKEAcgDBIQBAgGLARkYCy8oBTJlBCUocxQn0QUBDkkGxgNZQq0BZSbeAmIDgAEBOgGtAaMCDAOQAZ4BBIEBKUtQUYYBQscDDxPSARA1oAEHAWmnAsMB2wFyywGLAxol+wImlwOOA80CtwN26A0WjwJVbQEJPAH+BRDeAfkHK/ABASEBCSAaHQemAzkaRiu2Ad8BdXeiAwEBGBUBBN4LEIABK4gB2AFLfwECAdoENq0CkQGMBsIBiQEtiwGgA1zyAUQ4uwS8AwhsvgPyAcEDF27vApsBHaICGhl3GSKxAR8MC6cBAgItmQYG9QIeywLvAeYBDArLAh8HASI4ELICDVmVBgsY/gHWARtcAsMBpALiAdsBA7QBpAJmIArpByn0AyAKBwHTARIHAX8D+AMBcRIBBbEDmwUBMacCHAciNp0BAQF0OgQLJDuSAh54kwFSP0eeAQQ4M5EBQgMEmwFXywFo0gFyWwMcapQBBugBPUW2AVgBKmy3AR6PAbMBGQxrUJECvQR+8gFoWDsYgQNwRSczBRXQAgtRswEW0ALMAREYAUEBIG6yATYCRE8OxgER8gMBvQEDRkwLc8MBTwHZAUOnAXiiBakDIbYBNNcCIUmuArIBSakBrgFHKs0EgwV/G3AD0wE6LgECtQJ4xQFwFbUCjQPkBS6vAQqEAUZF3QIM9wEhCoYCQhXsBCyZArQDugIziALWAdIBlQHwBdUErQE6qQaSA4EEIvYBHir9AQVLmgMCApsCKAwHuwgrENsBAjNYswEVmgIt7QJnN4wDEnta+wGfAcUBxgEtEFXQAQWdAUAeBcwBAQM7rAEJATJ0LENrdh73A6UBhAE+qwEeASxLZUMhDREuH0CGARbd7K0GlQo"
const gplayPhenotype = "H4sIAAAAAAAAAB3OO3KjMAAA0KRNuWXukBkBQkAJ2MhgAZb5u2GCwQZbCH_EJ77QHmgvtDtbv-Z9_H63zXXU0NVPB1odlyGy7751Q3CitlPDvFd8lxhz3tpNmz7P92CFw73zdHU2Ie0Ad2kmR8lxhiErTFLt3RPGfJQHSDy7Clw10bg8kqf2owLokN4SecJTLoSwBnzQSd652_MOf2d1vKBNVedzg4ciPoLz2mQ8efGAgYeLou-l-PXn_7Sna1MfhHuySxt-4esulEDp8Sbq54CPPKjpANW-lkU2IZ0F92LBI-ukCKSptqeq1eXU96LD9nZfhKHdtjSWwJqUm_2r6pMHOxk01saVanmNopjX3YxQafC4iC6T55aRbC8nTI98AF_kItIQAJb5EQxnKTO7TZDWnr01HVPxelb9A2OWX6poidMWl16K54kcu_jhXw-JSBQkVcD_fPsLSZu6joIBAAA"
