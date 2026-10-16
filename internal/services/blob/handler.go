package blob

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/storageauth"
	"github.com/moabukar/miniblue/internal/store"
)

type Container struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type Blob struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
	Content    []byte            `json:"-"`
}

type Handler struct {
	store  *store.Store
	leases *leaseManager
}

func blobHTTPTime(v string) string {
	if v == "" {
		return time.Now().UTC().Format(http.TimeFormat)
	}
	if t, err := http.ParseTime(v); err == nil {
		return t.UTC().Format(http.TimeFormat)
	}
	if t, err := time.Parse(time.RFC1123, v); err == nil {
		return t.UTC().Format(http.TimeFormat)
	}
	return v
}

func blobLastModified(b Blob) string {
	return blobHTTPTime(b.Properties["lastModified"])
}

func blobCreationTime(b Blob) string {
	if v := b.Properties["creationTime"]; v != "" {
		return blobHTTPTime(v)
	}
	return blobLastModified(b)
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s, leases: newLeaseManager()}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/blob/{accountName}", func(r chi.Router) {
		r.Use(h.blobSharedKeyAuth)
		r.Get("/", h.blobAccountRoot)
		r.Head("/", h.blobAccountRoot)
		r.Route("/{containerName}", func(r chi.Router) {
			r.Put("/", h.CreateContainer)
			r.Get("/", h.ListBlobs)
			r.Delete("/", h.DeleteContainer)
			// Blob names in Azure may contain forward slashes (e.g.
			// "env:/prod/terraform.tfstate"), so the blob segment must be a
			// catch-all wildcard rather than chi's default single-segment
			// "{blobName}" parameter. Without this, requests like
			// /blob/{account}/{container}/foo/bar fall through chi's route
			// tree and are handled by the parent ARM mux's NotFound, which
			// returns an "InvalidResourceType" JSON error and breaks
			// Terraform's azurerm backend (issue #14).
			r.Put("/*", h.UploadBlob)
			r.Get("/*", h.DownloadBlob)
			r.Head("/*", h.HeadBlob)
			r.Delete("/*", h.DeleteBlob)
		})
	})
}

// envDisableSharedKeyAuth, when set to a truthy value (1, true, yes, on),
// causes the blob data-plane SharedKey middleware to skip signature
// verification entirely. This is intended for tutorials, smoke tests, and
// other dev workflows where issuing a fully SharedKey-signed request from a
// shell script would add disproportionate friction. It must NOT be enabled
// in any environment that pretends to model production.
const envDisableSharedKeyAuth = "MINIBLUE_DISABLE_SHAREDKEY_AUTH"

func sharedKeyAuthDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envDisableSharedKeyAuth))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (h *Handler) blobSharedKeyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sharedKeyAuthDisabled() {
			next.ServeHTTP(w, r)
			return
		}
		account := chi.URLParam(r, "accountName")
		k1, k2, hasKeys := storageauth.AccountKeyBytes(h.store, account)
		if !hasKeys {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if storageauth.VerifyBlobSharedKey(r, account, k1, k2) {
			next.ServeHTTP(w, r)
			return
		}
		writeBlobAuthFailure(w, r)
	})
}

func writeBlobAuthFailure(w http.ResponseWriter, r *http.Request) {
	reqID := uuid.New().String()
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("x-ms-request-id", reqID)
	w.WriteHeader(http.StatusForbidden)
	msg := "Server failed to authenticate the request. Make sure the value of Authorization header is formed correctly including the signature."
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8" standalone="yes"?>`+
		`<Error><Code>AuthenticationFailed</Code><Message>%s`+
		`RequestId:%s`+
		`Time:%s</Message></Error>`, msg, reqID, time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z"))
}

func (h *Handler) blobAccountRoot(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	comp := q.Get("comp")
	restype := q.Get("restype")

	switch {
	case comp == "properties" && restype == "service":
		h.getBlobServiceProperties(w, r)
	case comp == "list" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.listContainersXML(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) getBlobServiceProperties(w http.ResponseWriter, r *http.Request) {
	h.setBlobResponseHeaders(w, r)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	const xmlBody = `<?xml version="1.0" encoding="utf-8"?>
<StorageServiceProperties>
  <Logging><Version>1.0</Version><Read>false</Read><Write>false</Write><RetentionPolicy><Enabled>false</Enabled></RetentionPolicy></Logging>
  <HourMetrics><Version>1.0</Version><Enabled>false</Enabled><RetentionPolicy><Enabled>false</Enabled></RetentionPolicy></HourMetrics>
  <MinuteMetrics><Version>1.0</Version><Enabled>false</Enabled><RetentionPolicy><Enabled>false</Enabled></RetentionPolicy></MinuteMetrics>
  <Cors />
  <DefaultServiceVersion>2021-12-02</DefaultServiceVersion>
</StorageServiceProperties>`
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xmlBody))
}

func (h *Handler) setBlobResponseHeaders(w http.ResponseWriter, r *http.Request) {
	if v := r.Header.Get("X-Ms-Version"); v != "" {
		w.Header().Set("x-ms-version", v)
	}
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
}

type enumerationResults struct {
	XMLName         xml.Name        `xml:"EnumerationResults"`
	ServiceEndpoint string          `xml:"ServiceEndpoint,attr"`
	Containers      containersBlock `xml:"Containers"`
}

type containersBlock struct {
	Items []containerXML `xml:"Container"`
}

type containerXML struct {
	Name       string            `xml:"Name"`
	Properties containerPropsXML `xml:"Properties"`
}

type containerPropsXML struct {
	LastModified string `xml:"Last-Modified"`
	Etag         string `xml:"Etag"`
}

func (h *Handler) listContainersXML(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	prefix := "blob:container:" + account + ":"
	items := h.store.ListByPrefix(prefix)

	var names []string
	seen := map[string]bool{}
	for _, v := range items {
		if c, ok := v.(Container); ok && c.Name != "" {
			if !seen[c.Name] {
				seen[c.Name] = true
				names = append(names, c.Name)
			}
		}
	}
	sort.Strings(names)

	h.setBlobResponseHeaders(w, r)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	endpoint := "https://" + account + ".blob.core.windows.net/"
	res := enumerationResults{
		ServiceEndpoint: endpoint,
		Containers:      containersBlock{Items: make([]containerXML, 0, len(names))},
	}
	now := time.Now().UTC().Format(http.TimeFormat)
	for _, n := range names {
		res.Containers.Items = append(res.Containers.Items, containerXML{
			Name: n,
			Properties: containerPropsXML{
				LastModified: now,
				Etag:         "\"0x8" + fmt.Sprintf("%X", time.Now().UnixNano()) + "\"",
			},
		})
	}
	out, err := xml.MarshalIndent(res, "", "  ")
	if err != nil {
		azerr.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}

func (h *Handler) containerKey(account, container string) string {
	return "blob:container:" + account + ":" + container
}

func (h *Handler) blobKey(account, container, blob string) string {
	return "blob:blob:" + account + ":" + container + ":" + blob
}

// blobNameParam extracts the blob name from the request URL. Blob names may
// contain forward slashes, so the route uses a "/*" catch-all wildcard which
// chi exposes via the "*" URL parameter. Decoding it lets encoded slash paths
// like ".pulumi%2Fmeta.yaml" map to the same blob as their decoded form.
func blobNameParam(r *http.Request) string {
	name := chi.URLParam(r, "*")
	if decoded, err := url.PathUnescape(name); err == nil {
		return decoded
	}
	return name
}

func (h *Handler) CreateContainer(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")

	c := Container{
		Name: container,
		Properties: map[string]string{
			"lastModified": time.Now().UTC().Format(time.RFC1123),
			"etag":         fmt.Sprintf("\"0x%X\"", time.Now().UnixNano()),
		},
	}
	h.store.Set(h.containerKey(account, container), c)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) DeleteContainer(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	h.store.Delete(h.containerKey(account, container))
	h.store.DeleteByPrefix("blob:blob:" + account + ":" + container + ":")
	w.WriteHeader(http.StatusAccepted)
}

// blobListEnumerationResults models the List Blobs response envelope as
// described by the official Azure Storage REST API:
// https://learn.microsoft.com/en-us/rest/api/storageservices/list-blobs
type blobListEnumerationResults struct {
	XMLName         xml.Name   `xml:"EnumerationResults"`
	ServiceEndpoint string     `xml:"ServiceEndpoint,attr"`
	ContainerName   string     `xml:"ContainerName,attr"`
	Prefix          string     `xml:"Prefix"`
	Marker          string     `xml:"Marker"`
	MaxResults      int        `xml:"MaxResults"`
	Delimiter       string     `xml:"Delimiter"`
	Blobs           blobsBlock `xml:"Blobs"`
	NextMarker      string     `xml:"NextMarker"`
}

type blobsBlock struct {
	Items    []blobXML       `xml:"Blob"`
	Prefixes []blobPrefixXML `xml:"BlobPrefix"`
}

type blobXML struct {
	Name       string       `xml:"Name"`
	Properties blobPropsXML `xml:"Properties"`
}

type blobPrefixXML struct {
	Name string `xml:"Name"`
}

type blobPropsXML struct {
	LastModified  string `xml:"Last-Modified"`
	Etag          string `xml:"Etag"`
	ContentLength int64  `xml:"Content-Length"`
	ContentType   string `xml:"Content-Type"`
	BlobType      string `xml:"BlobType"`
	LeaseStatus   string `xml:"LeaseStatus"`
	LeaseState    string `xml:"LeaseState"`
}

func (h *Handler) ListBlobs(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	prefix := "blob:blob:" + account + ":" + container + ":"
	items := h.store.ListByPrefix(prefix)

	q := r.URL.Query()
	reqPrefix := q.Get("prefix")
	marker := q.Get("marker")
	delimiter := q.Get("delimiter")
	maxResults := 0
	if v := q.Get("maxresults"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxResults = n
		}
	}

	blobs := make([]Blob, 0, len(items))
	for _, v := range items {
		b, ok := v.(Blob)
		if !ok || b.Name == "" {
			continue
		}
		if reqPrefix != "" && !strings.HasPrefix(b.Name, reqPrefix) {
			continue
		}
		blobs = append(blobs, b)
	}
	sort.Slice(blobs, func(i, j int) bool { return blobs[i].Name < blobs[j].Name })

	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if host == "" {
		host = account + ".blob.core.windows.net"
	}
	endpoint := scheme + "://" + host + "/" + account + "/"

	res := blobListEnumerationResults{
		ServiceEndpoint: endpoint,
		ContainerName:   container,
		Prefix:          reqPrefix,
		Marker:          marker,
		MaxResults:      maxResults,
		Delimiter:       delimiter,
		Blobs:           blobsBlock{Items: make([]blobXML, 0, len(blobs))},
	}

	seenPrefixes := map[string]bool{}
	for _, b := range blobs {
		if delimiter != "" {
			remaining := strings.TrimPrefix(b.Name, reqPrefix)
			if idx := strings.Index(remaining, delimiter); idx >= 0 {
				prefixName := reqPrefix + remaining[:idx+len(delimiter)]
				if !seenPrefixes[prefixName] {
					res.Blobs.Prefixes = append(res.Blobs.Prefixes, blobPrefixXML{Name: prefixName})
					seenPrefixes[prefixName] = true
				}
				continue
			}
		}

		lastModified := b.Properties["lastModified"]
		if lastModified == "" {
			lastModified = time.Now().UTC().Format(http.TimeFormat)
		}
		etag := b.Properties["etag"]
		ct := b.Properties["contentType"]
		if ct == "" {
			ct = "application/octet-stream"
		}
		var contentLength int64
		if v := b.Properties["contentLength"]; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				contentLength = n
			}
		} else {
			contentLength = int64(len(b.Content))
		}
		leaseStatus, leaseState, _, _ := h.leases.snapshot(account, container, b.Name)
		if leaseStatus == "" {
			leaseStatus = "unlocked"
		}
		if leaseState == "" {
			leaseState = "available"
		}
		res.Blobs.Items = append(res.Blobs.Items, blobXML{
			Name: b.Name,
			Properties: blobPropsXML{
				LastModified:  lastModified,
				Etag:          etag,
				ContentLength: contentLength,
				ContentType:   ct,
				BlobType:      "BlockBlob",
				LeaseStatus:   leaseStatus,
				LeaseState:    leaseState,
			},
		})
	}

	h.setBlobResponseHeaders(w, r)
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-ms-request-id", uuid.New().String())

	out, err := xml.MarshalIndent(res, "", "  ")
	if err != nil {
		azerr.WriteError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(out)
}

func (h *Handler) UploadBlob(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	blobName := blobNameParam(r)

	switch r.URL.Query().Get("comp") {
	case "lease":
		h.handleLease(w, r)
		return
	case "metadata":
		h.setBlobMetadata(w, r, account, container, blobName)
		return
	case "properties":
		h.setBlobProperties(w, r, account, container, blobName)
		return
	}

	// Enforce existing lease for Put Blob (overwrite of a leased blob).
	if status, code, msg, ok := h.leases.checkAccess(account, container, blobName, r.Header.Get("x-ms-lease-id")); !ok {
		writeBlobLeaseError(w, status, code, msg)
		return
	}

	if copySource := r.Header.Get("x-ms-copy-source"); copySource != "" {
		h.copyBlob(w, r, account, container, blobName, copySource)
		return
	}

	data, _ := io.ReadAll(r.Body)
	// Per Put Blob spec, x-ms-blob-content-type takes precedence over
	// the request's Content-Type header for the stored blob's content type.
	ct := r.Header.Get("x-ms-blob-content-type")
	if ct == "" {
		ct = r.Header.Get("Content-Type")
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	now := time.Now().UTC()
	httpTime := now.Format(http.TimeFormat)
	b := Blob{
		Name: blobName,
		Properties: map[string]string{
			"creationTime":  httpTime,
			"lastModified":  httpTime,
			"contentLength": fmt.Sprintf("%d", len(data)),
			"contentType":   ct,
			"etag":          fmt.Sprintf("\"0x%X\"", now.UnixNano()),
		},
		Content: data,
	}
	// Apply x-ms-blob-content-* / x-ms-blob-cache-control system headers.
	if v := r.Header.Get("x-ms-blob-content-encoding"); v != "" {
		b.Properties["contentEncoding"] = v
	}
	if v := r.Header.Get("x-ms-blob-content-language"); v != "" {
		b.Properties["contentLanguage"] = v
	}
	if v := r.Header.Get("x-ms-blob-content-disposition"); v != "" {
		b.Properties["contentDisposition"] = v
	}
	if v := r.Header.Get("x-ms-blob-cache-control"); v != "" {
		b.Properties["cacheControl"] = v
	}
	if v := r.Header.Get("x-ms-blob-content-md5"); v != "" {
		b.Properties["contentMD5"] = v
	}
	// Apply x-ms-meta-* user metadata. Per Azure spec, Put Blob *replaces*
	// any existing metadata wholesale; since this is a fresh Blob value the
	// map starts empty so we just copy the request headers over.
	for name, vals := range r.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-ms-meta-") && len(vals) > 0 {
			b.Properties[lower] = vals[0]
		}
	}
	h.store.Set(h.blobKey(account, container, blobName), b)
	w.Header().Set("ETag", b.Properties["etag"])
	w.Header().Set("Last-Modified", b.Properties["lastModified"])
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.WriteHeader(http.StatusCreated)
}

func parseCopySource(source string) (account, container, blobName string, ok bool) {
	u, err := url.Parse(source)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(u.EscapedPath(), "/"), "/", 4)
	if len(parts) != 4 || parts[0] != "blob" {
		return "", "", "", false
	}
	account, err = url.PathUnescape(parts[1])
	if err != nil {
		return "", "", "", false
	}
	container, err = url.PathUnescape(parts[2])
	if err != nil {
		return "", "", "", false
	}
	blobName, err = url.PathUnescape(parts[3])
	if err != nil {
		return "", "", "", false
	}
	return account, container, blobName, true
}

func (h *Handler) copyBlob(w http.ResponseWriter, r *http.Request, account, container, blobName, source string) {
	srcAccount, srcContainer, srcBlobName, ok := parseCopySource(source)
	if !ok {
		writeBlobLeaseError(w, http.StatusBadRequest, "InvalidHeaderValue", "The x-ms-copy-source header value is invalid.")
		return
	}
	v, ok := h.store.Get(h.blobKey(srcAccount, srcContainer, srcBlobName))
	if !ok {
		writeBlobNotFound(w)
		return
	}
	src := v.(Blob)

	now := time.Now().UTC()
	httpTime := now.Format(http.TimeFormat)
	props := make(map[string]string, len(src.Properties)+2)
	for k, v := range src.Properties {
		props[k] = v
	}
	props["creationTime"] = httpTime
	props["lastModified"] = httpTime
	props["contentLength"] = fmt.Sprintf("%d", len(src.Content))
	props["etag"] = fmt.Sprintf("\"0x%X\"", now.UnixNano())
	props["copyID"] = uuid.New().String()
	props["copyStatus"] = "success"

	b := Blob{
		Name:       blobName,
		Properties: props,
		Content:    append([]byte(nil), src.Content...),
	}
	h.store.Set(h.blobKey(account, container, blobName), b)

	h.setBlobResponseHeaders(w, r)
	w.Header().Set("ETag", b.Properties["etag"])
	w.Header().Set("Last-Modified", b.Properties["lastModified"])
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.Header().Set("x-ms-copy-id", b.Properties["copyID"])
	w.Header().Set("x-ms-copy-status", b.Properties["copyStatus"])
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusAccepted)
}

// setBlobMetadata implements PUT /{container}/{blob}?comp=metadata.
// It persists x-ms-meta-* request headers without touching the blob content.
func (h *Handler) setBlobMetadata(w http.ResponseWriter, r *http.Request, account, container, blobName string) {
	if status, code, msg, ok := h.leases.checkAccess(account, container, blobName, r.Header.Get("x-ms-lease-id")); !ok {
		writeBlobLeaseError(w, status, code, msg)
		return
	}

	v, ok := h.store.Get(h.blobKey(account, container, blobName))
	if !ok {
		azerr.NotFound(w, "blob", blobName)
		return
	}
	b := v.(Blob)

	// Clear old metadata entries and set new ones from request headers.
	for k := range b.Properties {
		if strings.HasPrefix(k, "x-ms-meta-") {
			delete(b.Properties, k)
		}
	}
	for name, vals := range r.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-ms-meta-") && len(vals) > 0 {
			b.Properties[lower] = vals[0]
		}
	}

	now := time.Now()
	b.Properties["lastModified"] = now.UTC().Format(time.RFC1123)
	b.Properties["etag"] = fmt.Sprintf("\"0x%X\"", now.UnixNano())
	h.store.Set(h.blobKey(account, container, blobName), b)

	h.setBlobResponseHeaders(w, r)
	w.Header().Set("ETag", b.Properties["etag"])
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.WriteHeader(http.StatusOK)
}

// setBlobProperties implements PUT /{container}/{blob}?comp=properties.
// It updates blob content headers without touching the blob content.
func (h *Handler) setBlobProperties(w http.ResponseWriter, r *http.Request, account, container, blobName string) {
	if status, code, msg, ok := h.leases.checkAccess(account, container, blobName, r.Header.Get("x-ms-lease-id")); !ok {
		writeBlobLeaseError(w, status, code, msg)
		return
	}

	v, ok := h.store.Get(h.blobKey(account, container, blobName))
	if !ok {
		azerr.NotFound(w, "blob", blobName)
		return
	}
	b := v.(Blob)

	if ct := r.Header.Get("x-ms-blob-content-type"); ct != "" {
		b.Properties["contentType"] = ct
	}
	if ce := r.Header.Get("x-ms-blob-content-encoding"); ce != "" {
		b.Properties["contentEncoding"] = ce
	}
	if cl := r.Header.Get("x-ms-blob-content-language"); cl != "" {
		b.Properties["contentLanguage"] = cl
	}
	if cd := r.Header.Get("x-ms-blob-content-disposition"); cd != "" {
		b.Properties["contentDisposition"] = cd
	}
	if cc := r.Header.Get("x-ms-blob-cache-control"); cc != "" {
		b.Properties["cacheControl"] = cc
	}

	now := time.Now()
	b.Properties["lastModified"] = now.UTC().Format(time.RFC1123)
	b.Properties["etag"] = fmt.Sprintf("\"0x%X\"", now.UnixNano())
	h.store.Set(h.blobKey(account, container, blobName), b)

	h.setBlobResponseHeaders(w, r)
	w.Header().Set("ETag", b.Properties["etag"])
	w.Header().Set("Last-Modified", now.UTC().Format(http.TimeFormat))
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.WriteHeader(http.StatusOK)
}

// applyLeaseHeaders writes the x-ms-lease-* headers on a Get/Head Blob
// response so clients (including Terraform's azurerm backend) can observe the
// real lease state.
func (h *Handler) applyLeaseHeaders(w http.ResponseWriter, account, container, blob string) {
	status, state, dur, _ := h.leases.snapshot(account, container, blob)
	w.Header().Set("x-ms-lease-status", status)
	w.Header().Set("x-ms-lease-state", state)
	if dur != "" {
		w.Header().Set("x-ms-lease-duration", dur)
	}
}

// applyMetaHeaders emits all stored x-ms-meta-* entries as response headers.
func (h *Handler) applyMetaHeaders(w http.ResponseWriter, b Blob) {
	for k, v := range b.Properties {
		if strings.HasPrefix(k, "x-ms-meta-") {
			w.Header().Set(k, v)
		}
	}
}

// applyContentHeaders emits the system content headers (Content-Encoding,
// Content-Language, Content-Disposition, Cache-Control, Content-MD5) on
// Get/Head Blob responses when the blob has corresponding properties set.
func (h *Handler) applyContentHeaders(w http.ResponseWriter, b Blob) {
	if v := b.Properties["contentEncoding"]; v != "" {
		w.Header().Set("Content-Encoding", v)
	}
	if v := b.Properties["contentLanguage"]; v != "" {
		w.Header().Set("Content-Language", v)
	}
	if v := b.Properties["contentDisposition"]; v != "" {
		w.Header().Set("Content-Disposition", v)
	}
	if v := b.Properties["cacheControl"]; v != "" {
		w.Header().Set("Cache-Control", v)
	}
	if v := b.Properties["contentMD5"]; v != "" {
		w.Header().Set("Content-MD5", v)
	}
}

// writeBlobNotFound emits a clean Azure-compatible 404 response for HEAD/GET
// against a non-existent blob. It mirrors what real Azure Blob Storage
// returns: x-ms-error-code: BlobNotFound, a deterministic body, and an
// explicit Content-Length so the response is *fully terminated* (no chunked
// stream left dangling). Without an explicit Content-Length, Go's net/http
// server falls back to Transfer-Encoding: chunked; on a HEAD request it then
// sends headers but no terminating zero-length chunk, which causes
// well-behaved clients (Go's net/http, curl, …) to hang forever waiting for
// the body. See https://github.com/lonegunmanb/miniblue/issues for the
// Terraform azurerm backend reproduction.
func writeBlobNotFound(w http.ResponseWriter) {
	const body = `<?xml version="1.0" encoding="utf-8"?><Error><Code>BlobNotFound</Code><Message>The specified blob does not exist.</Message></Error>`
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("x-ms-error-code", "BlobNotFound")
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(body))
}

func (h *Handler) DownloadBlob(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	blobName := blobNameParam(r)

	v, ok := h.store.Get(h.blobKey(account, container, blobName))
	if !ok {
		writeBlobNotFound(w)
		return
	}
	b := v.(Blob)
	w.Header().Set("Content-Type", b.Properties["contentType"])
	w.Header().Set("Content-Length", b.Properties["contentLength"])
	w.Header().Set("ETag", b.Properties["etag"])
	w.Header().Set("Last-Modified", blobLastModified(b))
	w.Header().Set("x-ms-creation-time", blobCreationTime(b))
	w.Header().Set("x-ms-blob-type", "BlockBlob")
	w.Header().Set("x-ms-request-id", uuid.New().String())
	h.applyLeaseHeaders(w, account, container, blobName)
	h.applyMetaHeaders(w, b)
	h.applyContentHeaders(w, b)
	w.Write(b.Content)
}

// HeadBlob implements Get Blob Properties (HEAD on the blob). The Terraform
// azurerm backend issues this before acquiring a lease in order to discover
// the blob's etag and current lease state.
func (h *Handler) HeadBlob(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	blobName := blobNameParam(r)

	v, ok := h.store.Get(h.blobKey(account, container, blobName))
	if !ok {
		writeBlobNotFound(w)
		return
	}
	b := v.(Blob)
	w.Header().Set("Content-Type", b.Properties["contentType"])
	w.Header().Set("Content-Length", b.Properties["contentLength"])
	w.Header().Set("ETag", b.Properties["etag"])
	w.Header().Set("Last-Modified", blobLastModified(b))
	w.Header().Set("x-ms-creation-time", blobCreationTime(b))
	w.Header().Set("x-ms-blob-type", "BlockBlob")
	w.Header().Set("x-ms-request-id", uuid.New().String())
	w.Header().Set("Connection", "close")
	h.applyLeaseHeaders(w, account, container, blobName)
	h.applyMetaHeaders(w, b)
	h.applyContentHeaders(w, b)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DeleteBlob(w http.ResponseWriter, r *http.Request) {
	account := chi.URLParam(r, "accountName")
	container := chi.URLParam(r, "containerName")
	blobName := blobNameParam(r)

	if status, code, msg, ok := h.leases.checkAccess(account, container, blobName, r.Header.Get("x-ms-lease-id")); !ok {
		writeBlobLeaseError(w, status, code, msg)
		return
	}
	h.store.Delete(h.blobKey(account, container, blobName))
	w.WriteHeader(http.StatusAccepted)
}
