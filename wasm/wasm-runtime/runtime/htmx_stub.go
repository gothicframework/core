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

// HtmxEvent is a string-backed htmx event name (full htmx 2.0.3 catalog).
type HtmxEvent string

const (
	EvtAbort                     HtmxEvent = "htmx:abort"
	EvtAfterOnLoad               HtmxEvent = "htmx:afterOnLoad"
	EvtAfterProcessNode          HtmxEvent = "htmx:afterProcessNode"
	EvtAfterRequest              HtmxEvent = "htmx:afterRequest"
	EvtAfterSettle               HtmxEvent = "htmx:afterSettle"
	EvtAfterSwap                 HtmxEvent = "htmx:afterSwap"
	EvtBadResponseURL            HtmxEvent = "htmx:badResponseUrl"
	EvtBeforeCleanupElement      HtmxEvent = "htmx:beforeCleanupElement"
	EvtBeforeHistorySave         HtmxEvent = "htmx:beforeHistorySave"
	EvtBeforeHistoryUpdate       HtmxEvent = "htmx:beforeHistoryUpdate"
	EvtBeforeOnLoad              HtmxEvent = "htmx:beforeOnLoad"
	EvtBeforeProcessNode         HtmxEvent = "htmx:beforeProcessNode"
	EvtBeforeRequest             HtmxEvent = "htmx:beforeRequest"
	EvtBeforeSend                HtmxEvent = "htmx:beforeSend"
	EvtBeforeSwap                HtmxEvent = "htmx:beforeSwap"
	EvtBeforeTransition          HtmxEvent = "htmx:beforeTransition"
	EvtConfigRequest             HtmxEvent = "htmx:configRequest"
	EvtConfirm                   HtmxEvent = "htmx:confirm"
	EvtError                     HtmxEvent = "htmx:error"
	EvtEvalDisallowedError       HtmxEvent = "htmx:evalDisallowedError"
	EvtEventFilterError          HtmxEvent = "htmx:eventFilter:error"
	EvtHistoryCacheError         HtmxEvent = "htmx:historyCacheError"
	EvtHistoryCacheMiss          HtmxEvent = "htmx:historyCacheMiss"
	EvtHistoryCacheMissLoad      HtmxEvent = "htmx:historyCacheMissLoad"
	EvtHistoryCacheMissLoadError HtmxEvent = "htmx:historyCacheMissLoadError"
	EvtHistoryItemCreated        HtmxEvent = "htmx:historyItemCreated"
	EvtHistoryRestore            HtmxEvent = "htmx:historyRestore"
	EvtInvalidPath               HtmxEvent = "htmx:invalidPath"
	EvtLoad                      HtmxEvent = "htmx:load"
	EvtOnLoadError               HtmxEvent = "htmx:onLoadError"
	EvtOobAfterSwap              HtmxEvent = "htmx:oobAfterSwap"
	EvtOobBeforeSwap             HtmxEvent = "htmx:oobBeforeSwap"
	EvtOobErrorNoTarget          HtmxEvent = "htmx:oobErrorNoTarget"
	EvtPrompt                    HtmxEvent = "htmx:prompt"
	EvtPushedIntoHistory         HtmxEvent = "htmx:pushedIntoHistory"
	EvtReplacedInHistory         HtmxEvent = "htmx:replacedInHistory"
	EvtResponseError             HtmxEvent = "htmx:responseError"
	EvtRestored                  HtmxEvent = "htmx:restored"
	EvtSendAbort                 HtmxEvent = "htmx:sendAbort"
	EvtSendError                 HtmxEvent = "htmx:sendError"
	EvtSwapError                 HtmxEvent = "htmx:swapError"
	EvtSyntaxError               HtmxEvent = "htmx:syntax:error"
	EvtTargetError               HtmxEvent = "htmx:targetError"
	EvtTimeout                   HtmxEvent = "htmx:timeout"
	EvtTrigger                   HtmxEvent = "htmx:trigger"
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
