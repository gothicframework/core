//go:build !js || !wasm

package runtime

// Host (server / test) no-op twin of htmx.go. It mirrors the js&&wasm HTMX
// surface exactly — same types, consts and method set — so generated main()s and
// ClientSideState bodies that call HTMX.* type-check and run harmlessly off the
// browser, where there is no window.htmx. Every method is a no-op returning a
// documented zero value; none panic.

// HTMX is the host no-op singleton (see the js&&wasm HTMX for behavior).
var HTMX htmxAPI

// htmxAPI is the unexported receiver type behind the HTMX singleton.
type htmxAPI struct{}

// Event is the browser Event passed to On/OnGlobal handlers (host: JSValue stub).
type Event = JSValue

// SwapStrategy is a string-backed htmx swap style (see js&&wasm for the vocabulary).
type SwapStrategy string

const (
	InnerHTML   SwapStrategy = "innerHTML"
	OuterHTML   SwapStrategy = "outerHTML"
	BeforeBegin SwapStrategy = "beforebegin"
	AfterBegin  SwapStrategy = "afterbegin"
	BeforeEnd   SwapStrategy = "beforeend"
	AfterEnd    SwapStrategy = "afterend"
	Delete      SwapStrategy = "delete"
	None        SwapStrategy = "none"
)

// HtmxEvent is a string-backed htmx event name. The consts below are the
// htmx 4.0 event surface of the port (renamed events keep their Go name and
// point at the 4.0 event string; constants whose event the 4.0 core no longer
// dispatches keep their legacy string and are marked as inert). A custom or
// extension event stays reachable via a string cast, e.g.
// HtmxEvent("htmx:sse:message").
type HtmxEvent string

const (
	EvtAbort          HtmxEvent = "htmx:abort"
	EvtAfterRequest   HtmxEvent = "htmx:after:request"
	EvtAfterSettle    HtmxEvent = "htmx:after:settle"
	EvtAfterSwap      HtmxEvent = "htmx:after:swap"
	EvtBeforeRequest  HtmxEvent = "htmx:before:request"
	EvtBeforeSwap     HtmxEvent = "htmx:before:swap"
	EvtConfigRequest  HtmxEvent = "htmx:config:request"
	EvtConfirm        HtmxEvent = "htmx:confirm"
	EvtError          HtmxEvent = "htmx:error"
	EvtFinallyRequest HtmxEvent = "htmx:finally:request"
	EvtFinallySwap    HtmxEvent = "htmx:finally:swap"
	EvtPrompt         HtmxEvent = "htmx:prompt"
	EvtTrigger        HtmxEvent = "htmx:trigger"
	EvtBeforeResponse HtmxEvent = "htmx:before:response"
	EvtResponseError  HtmxEvent = "htmx:response:error"

	// Element lifecycle (htmx 4.0 names; the htmx 2 process/load-family events
	// were renamed or dropped in 4.0 and the Go names keep working).
	EvtBeforeInit    HtmxEvent = "htmx:before:init"
	EvtAfterInit     HtmxEvent = "htmx:after:init"
	EvtBeforeProcess HtmxEvent = "htmx:before:process"
	EvtAfterProcess  HtmxEvent = "htmx:after:process"

	// Cleanup / history (renamed in 4.0).
	EvtBeforeCleanup        HtmxEvent = "htmx:before:cleanup"
	EvtAfterCleanup         HtmxEvent = "htmx:after:cleanup"
	EvtBeforeHistoryUpdate  HtmxEvent = "htmx:before:history:update"
	EvtAfterHistoryPush     HtmxEvent = "htmx:after:history:push"
	EvtAfterHistoryReplace  HtmxEvent = "htmx:after:history:replace"
	EvtBeforeHistoryRestore HtmxEvent = "htmx:before:history:restore"

	// Renamed but kept under their 2.x Go names (aliases of the replacements).
	EvtAfterProcessNode     HtmxEvent = "htmx:after:init"
	EvtBeforeProcessNode    HtmxEvent = "htmx:before:init"
	EvtAfterOnLoad          HtmxEvent = "htmx:after:request"
	EvtBeforeOnLoad         HtmxEvent = "htmx:before:response"
	EvtBeforeCleanupElement HtmxEvent = "htmx:before:cleanup"
	EvtHistoryRestore       HtmxEvent = "htmx:before:history:restore"
	EvtPushedIntoHistory    HtmxEvent = "htmx:after:history:push"
	EvtReplacedInHistory    HtmxEvent = "htmx:after:history:replace"
	EvtResponseErrorLegacy  HtmxEvent = "htmx:response:error"
	EvtBeforeSend           HtmxEvent = "htmx:before:request"

	// Inert in 4.0 (the event no longer fires; the constant remains so old code
	// compiles, and its string matches nothing the port dispatches).
	EvtBadResponseURL            HtmxEvent = "htmx:badResponseUrl"
	EvtBeforeHistorySave         HtmxEvent = "htmx:beforeHistorySave"
	EvtBeforeTransition          HtmxEvent = "htmx:beforeTransition"
	EvtEvalDisallowedError       HtmxEvent = "htmx:evalDisallowedError"
	EvtEventFilterError          HtmxEvent = "htmx:eventFilter:error"
	EvtHistoryCacheError         HtmxEvent = "htmx:historyCacheError"
	EvtHistoryCacheMiss          HtmxEvent = "htmx:historyCacheMiss"
	EvtHistoryCacheMissLoad      HtmxEvent = "htmx:historyCacheMissLoad"
	EvtHistoryCacheMissLoadError HtmxEvent = "htmx:historyCacheMissLoadError"
	EvtHistoryItemCreated        HtmxEvent = "htmx:historyItemCreated"
	EvtInvalidPath               HtmxEvent = "htmx:invalidPath"
	EvtLoad                      HtmxEvent = "htmx:load"
	EvtOnLoadError               HtmxEvent = "htmx:onLoadError"
	EvtOobAfterSwap              HtmxEvent = "htmx:oobAfterSwap"
	EvtOobBeforeSwap             HtmxEvent = "htmx:oobBeforeSwap"
	EvtOobErrorNoTarget          HtmxEvent = "htmx:oobErrorNoTarget"
	EvtRestored                  HtmxEvent = "htmx:restored"
	EvtSendAbort                 HtmxEvent = "htmx:sendAbort"
	EvtSendError                 HtmxEvent = "htmx:sendError"
	EvtSwapError                 HtmxEvent = "htmx:swapError"
	EvtSyntaxError               HtmxEvent = "htmx:syntax:error"
	EvtTargetError               HtmxEvent = "htmx:targetError"
	EvtTimeout                   HtmxEvent = "htmx:timeout"
	EvtValidateURL               HtmxEvent = "htmx:validateUrl"
	EvtValidationValidate        HtmxEvent = "htmx:validation:validate"
	EvtValidationFailed          HtmxEvent = "htmx:validation:failed"
	EvtValidationHalted          HtmxEvent = "htmx:validation:halted"
	EvtXHRAbort                  HtmxEvent = "htmx:xhr:abort"
	EvtXHRLoadstart              HtmxEvent = "htmx:xhr:loadstart"
	EvtXHRLoadend                HtmxEvent = "htmx:xhr:loadend"
	EvtXHRProgress               HtmxEvent = "htmx:xhr:progress"
)

// AjaxOpts is the optional context for HTMX.Ajax (see js&&wasm for field meaning).
type AjaxOpts struct {
	Target string
	Source string
	Swap   SwapStrategy
	Values map[string]string
}

func (htmxAPI) Ajax(method, url string, opts ...AjaxOpts)                    {}
func (htmxAPI) Swap(target, html string, s ...SwapStrategy)                  {}
func (htmxAPI) Process(target string)                                        {}
func (htmxAPI) Trigger(target string, e HtmxEvent, detail ...map[string]any) {}
func (htmxAPI) On(e HtmxEvent, h func(Event))                                {}
func (htmxAPI) OnGlobal(e HtmxEvent, h func(Event))                          {}
func (htmxAPI) Off(e HtmxEvent, h func(Event))                               {}
func (htmxAPI) AddClass(target, class string)                                {}
func (htmxAPI) RemoveClass(target, class string)                             {}
func (htmxAPI) ToggleClass(target, class string)                             {}
func (htmxAPI) TakeClass(target, class string)                               {}
func (htmxAPI) Find(sel string) JSValue                                      { return JSValue{} }
func (htmxAPI) FindAll(sel string) []JSValue                                 { return nil }
func (htmxAPI) Closest(target, sel string) JSValue                           { return JSValue{} }
func (htmxAPI) Values(target string) map[string]any                          { return map[string]any{} }
func (htmxAPI) Remove(target string)                                         {}
