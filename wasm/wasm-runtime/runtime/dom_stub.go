//go:build !js || !wasm

package runtime

func SetText(id, value string)            {}
func SetHTML(id, html string)             {}
func SetValue(id, value string)           {}
func GetValue(id string) string           { return "" }
func GetFileBytes(id string) []byte       { return nil }
func AddClass(id, className string)       {}
func RemoveClass(id, className string)    {}
func ToggleClass(id, className string)    {}
func SetAttr(id, attr, value string)      {}
func SetStyle(id, property, value string) {}

type FetchConfig struct {
	Method    string
	Headers   map[string]string
	Body      string
	BodyBytes []byte
	Query     map[string]string
}

type Response struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

func (r Response) Text() string  { return string(r.Body) }
func (r Response) Bytes() []byte { return r.Body }
func (r Response) OK() bool      { return r.Status >= 200 && r.Status < 300 }

// MapAny is a server-side no-op mirror of the WASM runtime method (Response has
// no body server-side). Returns nil, nil — the real JSON parse happens only in
// the browser build (dom.go). It is a METHOD, so parity_surface_test.go's
// top-level-func check does not cover it; it must be mirrored here by hand.
func (r Response) MapAny() (map[string]any, error) { return nil, nil }

type FetchResult struct {
	Response Response
	Err      error
}

func Fetch(url string, config ...FetchConfig) (Response, error) { return Response{}, nil }

func FetchAsync(url string, cfg FetchConfig, done func(Response, error)) {}

// FetchChan returns a buffered channel already carrying one zero FetchResult so
// a host consumer that receives from it gets a zero value instead of blocking
// forever — matching the zero-value discipline of the other server-side stubs.
func FetchChan(url string, cfg ...FetchConfig) <-chan FetchResult {
	ch := make(chan FetchResult, 1)
	ch <- FetchResult{}
	return ch
}

type JSValue struct{}

func JS() JSValue       { return JSValue{} }
func Window() JSValue   { return JSValue{} }
func Document() JSValue { return JSValue{} }

func ConsoleLog(args ...any) {}

func GetElementById(id string) JSValue    { return JSValue{} }
func CreateElement(tag string) JSValue    { return JSValue{} }
func QuerySelector(sel string) JSValue    { return JSValue{} }
func QuerySelectorAll(sel string) JSValue { return JSValue{} }

func (v JSValue) Get(key string) JSValue                  { return JSValue{} }
func (v JSValue) Set(key string, val any)                 {}
func (v JSValue) Call(method string, args ...any) JSValue { return JSValue{} }
func (v JSValue) New(args ...any) JSValue                 { return JSValue{} }
func (v JSValue) String() string                          { return "" }
func (v JSValue) Int() int                                { return 0 }
func (v JSValue) Float() float64                          { return 0 }
func (v JSValue) Bool() bool                              { return false }
func (v JSValue) IsNull() bool                            { return true }
func (v JSValue) IsUndefined() bool                       { return true }
func (v JSValue) Truthy() bool                            { return false }
func (v JSValue) Index(i int) JSValue                     { return JSValue{} }
func (v JSValue) SetIndex(i int, val any)                 {}
func (v JSValue) Length() int                             { return 0 }

func CopyBytesToJS(dst JSValue, src []byte) int { return 0 }
func CopyBytesToGo(dst []byte, src JSValue) int { return 0 }

func TriggerDownload(filename string, data []byte, mimeType string) {}

func AddEventListenerWithEvent(el JSValue, event string, fn func(JSValue)) {}
func AddEventListener(el JSValue, event string, fn func())                 {}

func AppendChild(parent, child JSValue) {}
func RemoveElement(el JSValue)          {}
func ClickElement(el JSValue)           {}

func WriteClipboard(text string) {}

func ExecJS(script string) {}

func Navigate(url string)         {}
func Reload()                     {}
func PushState(url, title string) {}
func GoBack()                     {}

type CookieOptions struct {
	MaxAge   int
	Path     string
	SameSite string
	Secure   bool
}

func LocalStorageSet(key, value string)                   {}
func LocalStorageGet(key string) string                   { return "" }
func LocalStorageRemove(key string)                       {}
func SessionStorageSet(key, value string)                 {}
func SessionStorageGet(key string) string                 { return "" }
func SessionStorageRemove(key string)                     {}
func CookieSet(key, value string, opts ...CookieOptions) {}
func CookieGet(key string) string                         { return "" }
func CookieDelete(key string)                             {}
