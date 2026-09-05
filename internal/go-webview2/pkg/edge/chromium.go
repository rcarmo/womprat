//go:build windows
// +build windows

package edge

import (
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"github.com/jchv/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

type Chromium struct {
	hwnd                  uintptr
	focusOnInit           bool
	controller            *ICoreWebView2Controller
	webview               *ICoreWebView2
	inited                uintptr
	envCompleted          *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	controllerCompleted   *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler
	webMessageReceived    *iCoreWebView2WebMessageReceivedEventHandler
	permissionRequested   *iCoreWebView2PermissionRequestedEventHandler
	webResourceRequested  *iCoreWebView2WebResourceRequestedEventHandler
	acceleratorKeyPressed *ICoreWebView2AcceleratorKeyPressedEventHandler
	navigationCompleted   *ICoreWebView2NavigationCompletedEventHandler

	environment *ICoreWebView2Environment

	// Settings
	DataPath string

	// permissions
	permissions      map[CoreWebView2PermissionKind]CoreWebView2PermissionState
	globalPermission *CoreWebView2PermissionState

	newWindowRequested *ICoreWebView2NewWindowRequestedEventHandler

	processFailed *ICoreWebView2ProcessFailedEventHandler

	// Callbacks
	ProcessFailedCallback        func(int32)
	NewWindowRequestedCallback   func(string)
	MessageCallback              func(string)
	WebResourceRequestedCallback func(request *ICoreWebView2WebResourceRequest, args *ICoreWebView2WebResourceRequestedEventArgs)
	NavigationCompletedCallback  func(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs)
	AcceleratorKeyCallback       func(uint) bool
}

func NewChromium() *Chromium {
	e := &Chromium{}
	/*
	 All these handlers are passed to native code through syscalls with 'uintptr(unsafe.Pointer(handler))' and we know
	 that a pointer to those will be kept in the native code. Furthermore these handlers als contain pointer to other Go
	 structs like the vtable.
	 This violates the unsafe.Pointer rule '(4) Conversion of a Pointer to a uintptr when calling syscall.Syscall.' because
	 theres no guarantee that Go doesn't move these objects.
	 AFAIK currently the Go runtime doesn't move HEAP objects, so we should be safe with these handlers. But they don't
	 guarantee it, because in the future Go might use a compacting GC.
	 There's a proposal to add a runtime.Pin function, to prevent moving pinned objects, which would allow to easily fix
	 this issue by just pinning the handlers. The https://go-review.googlesource.com/c/go/+/367296/ should land in Go 1.19.
	*/
	e.envCompleted = newICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler(e)
	e.controllerCompleted = newICoreWebView2CreateCoreWebView2ControllerCompletedHandler(e)
	e.webMessageReceived = newICoreWebView2WebMessageReceivedEventHandler(e)
	e.permissionRequested = newICoreWebView2PermissionRequestedEventHandler(e)
	e.webResourceRequested = newICoreWebView2WebResourceRequestedEventHandler(e)
	e.acceleratorKeyPressed = newICoreWebView2AcceleratorKeyPressedEventHandler(e)
	e.navigationCompleted = newICoreWebView2NavigationCompletedEventHandler(e)
	e.newWindowRequested = newICoreWebView2NewWindowRequestedEventHandler(e)
	e.processFailed = newICoreWebView2ProcessFailedEventHandler(e)
	e.permissions = make(map[CoreWebView2PermissionKind]CoreWebView2PermissionState)

	return e
}

func (e *Chromium) Embed(hwnd uintptr) bool {
	e.hwnd = hwnd

	dataPath := e.DataPath
	if dataPath == "" {
		currentExePath := make([]uint16, windows.MAX_PATH)
		_, err := windows.GetModuleFileName(windows.Handle(0), &currentExePath[0], windows.MAX_PATH)
		if err != nil {
			// What to do here?
			return false
		}
		currentExeName := filepath.Base(windows.UTF16ToString(currentExePath))
		dataPath = filepath.Join(os.Getenv("AppData"), currentExeName)
	}

	res, err := createCoreWebView2EnvironmentWithOptions(nil, windows.StringToUTF16Ptr(dataPath), 0, e.envCompleted)
	if err != nil {
		log.Printf("Error calling Webview2Loader: %v", err)
		return false
	} else if res != 0 {
		log.Printf("Result: %08x", res)
		return false
	}
	var msg w32.Msg
	for {
		if atomic.LoadUintptr(&e.inited) != 0 {
			break
		}
		r, _, _ := w32.User32GetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if r == 0 || int32(r) == -1 {
			break
		}
		_, _, _ = w32.User32TranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = w32.User32DispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	if atomic.LoadUintptr(&e.inited) != 1 || e.webview == nil {
		return false
	}
	e.Init("window.external={invoke:s=>window.chrome.webview.postMessage(s)}")
	return true
}

func (e *Chromium) Navigate(url string) {
	_, _, _ = e.webview.vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(url))),
	)
}

// GoBack navigates the WebView back in its session history using the native
// WebView2 navigation stack (more reliable than evaluating history.back()).
func (e *Chromium) GoBack() {
	_, _, _ = e.webview.vtbl.GoBack.Call(uintptr(unsafe.Pointer(e.webview)))
}

// GoForward navigates the WebView forward in its session history.
func (e *Chromium) GoForward() {
	_, _, _ = e.webview.vtbl.GoForward.Call(uintptr(unsafe.Pointer(e.webview)))
}

// Reload uses WebView2's native operation so reload also works on
// browser-generated error pages and cannot be blocked by page script policy.
func (e *Chromium) Reload() {
	if e.webview == nil {
		return
	}
	_, _, _ = e.webview.vtbl.Reload.Call(uintptr(unsafe.Pointer(e.webview)))
}

// CanGoBack reports whether back navigation is currently available.
func (e *Chromium) CanGoBack() bool {
	var can int32
	r, _, _ := e.webview.vtbl.GetCanGoBack.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(&can)),
	)
	if r != 0 {
		return false
	}
	return can != 0
}

// CanGoForward reports whether forward navigation is currently available.
func (e *Chromium) CanGoForward() bool {
	var can int32
	r, _, _ := e.webview.vtbl.GetCanGoForward.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(&can)),
	)
	if r != 0 {
		return false
	}
	return can != 0
}

// GetSource returns the WebView's current document URL. Unlike the URL we pass
// to Navigate(), this reflects the live location after in-page link clicks,
// redirects, and history navigation, so the shell address bar can stay in sync.
func (e *Chromium) GetSource() string {
	if e.webview == nil {
		return ""
	}
	var ptr *uint16
	_, _, _ = e.webview.vtbl.GetSource.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(&ptr)),
	)
	if ptr == nil {
		return ""
	}
	s := w32.Utf16PtrToString(ptr)
	windows.CoTaskMemFree(unsafe.Pointer(ptr))
	return s
}

func (e *Chromium) NavigateToString(htmlContent string) {
	_, _, _ = e.webview.vtbl.NavigateToString.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(htmlContent))),
	)
}

func (e *Chromium) Init(script string) {
	_, _, _ = e.webview.vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(script))),
		0,
	)
}

func (e *Chromium) Eval(script string) {
	_script, err := windows.UTF16PtrFromString(script)
	if err != nil {
		log.Printf("Invalid script: %v", err)
		return
	}
	if e.webview == nil {
		return
	}

	_, _, _ = e.webview.vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(_script)),
		0,
	)
}

func (e *Chromium) Show() error {
	return e.controller.PutIsVisible(true)
}

func (e *Chromium) Hide() error {
	return e.controller.PutIsVisible(false)
}

func (e *Chromium) QueryInterface(_, _ uintptr) uintptr {
	return 0
}

func (e *Chromium) AddRef() uintptr {
	return 1
}

func (e *Chromium) Release() uintptr {
	return 1
}

func (e *Chromium) EnvironmentCompleted(res uintptr, env *ICoreWebView2Environment) uintptr {
	if int32(res) < 0 || env == nil {
		log.Printf("Creating environment failed with %08x", res)
		atomic.StoreUintptr(&e.inited, 2)
		return 0
	}
	log.Printf("webview2: environment created (res=%08x); creating controller", res)
	_, _, _ = env.vtbl.AddRef.Call(uintptr(unsafe.Pointer(env)))
	e.environment = env

	hr, _, _ := env.vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(env)),
		e.hwnd,
		uintptr(unsafe.Pointer(e.controllerCompleted)),
	)
	if int32(hr) < 0 {
		log.Printf("Starting controller creation failed with %08x", hr)
		atomic.StoreUintptr(&e.inited, 2)
	}
	return 0
}

func (e *Chromium) CreateCoreWebView2ControllerCompleted(res uintptr, controller *ICoreWebView2Controller) uintptr {
	if int32(res) < 0 || controller == nil {
		log.Printf("Creating controller failed with %08x", res)
		atomic.StoreUintptr(&e.inited, 2)
		return 0
	}
	log.Printf("webview2: controller created (res=%08x)", res)
	_, _, _ = controller.vtbl.AddRef.Call(uintptr(unsafe.Pointer(controller)))
	e.controller = controller

	var token _EventRegistrationToken
	hr, _, _ := controller.vtbl.GetCoreWebView2.Call(
		uintptr(unsafe.Pointer(controller)),
		uintptr(unsafe.Pointer(&e.webview)),
	)
	if int32(hr) < 0 || e.webview == nil {
		log.Printf("Getting controller WebView failed with %08x", hr)
		atomic.StoreUintptr(&e.inited, 2)
		return 0
	}
	_, _, _ = e.webview.vtbl.AddRef.Call(
		uintptr(unsafe.Pointer(e.webview)),
	)
	_, _, _ = e.webview.vtbl.AddWebMessageReceived.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.webMessageReceived)),
		uintptr(unsafe.Pointer(&token)),
	)
	_, _, _ = e.webview.vtbl.AddPermissionRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.permissionRequested)),
		uintptr(unsafe.Pointer(&token)),
	)
	_, _, _ = e.webview.vtbl.AddWebResourceRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.webResourceRequested)),
		uintptr(unsafe.Pointer(&token)),
	)
	_, _, _ = e.webview.vtbl.AddNavigationCompleted.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.navigationCompleted)),
		uintptr(unsafe.Pointer(&token)),
	)

	if e.ProcessFailedCallback != nil {
		hr, _, _ := e.webview.vtbl.AddProcessFailed.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(e.processFailed)), uintptr(unsafe.Pointer(&token)))
		if int32(hr) < 0 {
			log.Printf("Register process failure handler failed: HRESULT %#x", hr)
			atomic.StoreUintptr(&e.inited, 2)
			return 0
		}
	}
	if e.NewWindowRequestedCallback != nil {
		hr, _, _ := e.webview.vtbl.AddNewWindowRequested.Call(
			uintptr(unsafe.Pointer(e.webview)),
			uintptr(unsafe.Pointer(e.newWindowRequested)),
			uintptr(unsafe.Pointer(&token)),
		)
		if int32(hr) < 0 {
			log.Printf("Register popup handler failed: HRESULT %#x", hr)
			atomic.StoreUintptr(&e.inited, 2)
			return 0
		}
	}
	_ = e.controller.AddAcceleratorKeyPressed(e.acceleratorKeyPressed, &token)

	atomic.StoreUintptr(&e.inited, 1)

	if e.focusOnInit {
		e.Focus()
	}

	return 0
}

func (e *Chromium) ProcessFailed(_ *ICoreWebView2, args *ICoreWebView2ProcessFailedEventArgs) uintptr {
	if e.ProcessFailedCallback != nil {
		e.ProcessFailedCallback(args.Kind())
	}
	return 0
}

func (e *Chromium) NewWindowRequested(_ *ICoreWebView2, args *ICoreWebView2NewWindowRequestedEventArgs) uintptr {
	// Suppress default popup creation even for an invalid or unreadable URI.
	if err := args.SetHandled(); err != nil {
		log.Printf("%v", err)
		return 0
	}
	uri, err := args.URI()
	if err != nil {
		log.Printf("%v", err)
		return 0
	}
	if e.NewWindowRequestedCallback != nil {
		e.NewWindowRequestedCallback(uri)
	}
	return 0
}

func (e *Chromium) MessageReceived(sender *ICoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr {
	var message *uint16
	hr, _, _ := args.vtbl.TryGetWebMessageAsString.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&message)),
	)
	if int32(hr) < 0 || message == nil {
		return 0
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(message))
	if e.MessageCallback != nil {
		e.MessageCallback(w32.Utf16PtrToString(message))
	}
	_, _, _ = sender.vtbl.PostWebMessageAsString.Call(
		uintptr(unsafe.Pointer(sender)),
		uintptr(unsafe.Pointer(message)),
	)
	return 0
}

func (e *Chromium) SetPermission(kind CoreWebView2PermissionKind, state CoreWebView2PermissionState) {
	e.permissions[kind] = state
}

func (e *Chromium) SetGlobalPermission(state CoreWebView2PermissionState) {
	e.globalPermission = &state
}

func (e *Chromium) PermissionRequested(_ *ICoreWebView2, args *iCoreWebView2PermissionRequestedEventArgs) uintptr {
	var kind CoreWebView2PermissionKind
	_, _, _ = args.vtbl.GetPermissionKind.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&kind)),
	)
	var result CoreWebView2PermissionState
	if e.globalPermission != nil {
		result = *e.globalPermission
	} else {
		var ok bool
		result, ok = e.permissions[kind]
		if !ok {
			result = CoreWebView2PermissionStateDefault
		}
	}
	_, _, _ = args.vtbl.PutState.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(result),
	)
	return 0
}

func (e *Chromium) WebResourceRequested(sender *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) uintptr {
	req, err := args.GetRequest()
	if err != nil {
		log.Printf("Get web resource request failed: %v", err)
		return 0
	}
	if e.WebResourceRequestedCallback != nil {
		e.WebResourceRequestedCallback(req, args)
	}
	return 0
}

func (e *Chromium) AddWebResourceRequestedFilter(filter string, ctx COREWEBVIEW2_WEB_RESOURCE_CONTEXT) {
	err := e.webview.AddWebResourceRequestedFilter(filter, ctx)
	if err != nil {
		log.Fatal(err)
	}
}

func (e *Chromium) Environment() *ICoreWebView2Environment {
	return e.environment
}

// AcceleratorKeyPressed is called when an accelerator key is pressed.
// If the AcceleratorKeyCallback method has been set, it will defer handling of the keypress
// to the callback. That callback returns a bool indicating if the event was handled.
func (e *Chromium) AcceleratorKeyPressed(sender *ICoreWebView2Controller, args *ICoreWebView2AcceleratorKeyPressedEventArgs) uintptr {
	if e.AcceleratorKeyCallback == nil {
		return 0
	}
	eventKind, _ := args.GetKeyEventKind()
	if eventKind == COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN ||
		eventKind == COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN {
		virtualKey, _ := args.GetVirtualKey()
		status, _ := args.GetPhysicalKeyStatus()
		if !status.WasKeyDown {
			_ = args.PutHandled(e.AcceleratorKeyCallback(virtualKey))
			return 0
		}
	}
	_ = args.PutHandled(false)
	return 0
}

func (e *Chromium) GetSettings() (*ICoreWebViewSettings, error) {
	return e.webview.GetSettings()
}

// PutAreBrowserAcceleratorKeysEnabled toggles WebView2's built-in browser
// accelerator keys (Ctrl+R/F5 reload, Ctrl+P print, etc.). Disabling lets the
// host application handle those keys instead.
func (e *Chromium) PutAreBrowserAcceleratorKeysEnabled(enabled bool) error {
	s, err := e.GetSettings()
	if err != nil {
		return err
	}
	return s.PutAreBrowserAcceleratorKeysEnabled(enabled)
}

func (e *Chromium) GetController() *ICoreWebView2Controller {
	return e.controller
}

func boolToInt(input bool) int {
	if input {
		return 1
	}
	return 0
}

func (e *Chromium) NavigationCompleted(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) uintptr {
	if e.NavigationCompletedCallback != nil {
		e.NavigationCompletedCallback(sender, args)
	}
	return 0
}

func (e *Chromium) NotifyParentWindowPositionChanged() error {
	//It looks like the wndproc function is called before the controller initialization is complete.
	//Because of this the controller is nil
	if e.controller == nil {
		return nil
	}
	return e.controller.NotifyParentWindowPositionChanged()
}

func (e *Chromium) Focus() {
	if e.controller == nil {
		e.focusOnInit = true
		return
	}
	_ = e.controller.MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC)
}
