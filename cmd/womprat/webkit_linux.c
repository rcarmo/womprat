//go:build linux && webkitgui

#include "webkit_linux.h"
#include <gtk/gtk.h>
#include <stdlib.h>
#include <string.h>
#include <webkit2/webkit2.h>

extern void goWompratDispatch(uintptr_t);
extern void goWompratMessage(uintptr_t, char *, char *);
extern void goWompratEvent(uintptr_t, char *, int, char *, char *, int, int);
extern void goWompratQueryResult(uintptr_t, char *);
extern void goWompratClosed(uintptr_t);

struct WompratGTK {
  uintptr_t handle;
  GtkWidget *window, *layout;
  WebKitWebView *shell;
  WebKitWebContext *content;
  GHashTable *tabs;
  char *active, *bridge, *shell_uri;
  int chrome, debug;
};
typedef struct {
  WompratGTK *host;
  char *id;
  WebKitWebView *view;
  gboolean failed;
  GTlsCertificate *certificate;
  char *certificate_uri;
} BrowserTab;
typedef struct {
  uintptr_t host;
  char *id, *url;
} CookieRequest;

static gboolean dispatch(gpointer p) {
  goWompratDispatch((uintptr_t)p);
  return G_SOURCE_REMOVE;
}
void womprat_gtk_dispatch(uintptr_t h) { g_idle_add(dispatch, (gpointer)h); }
void womprat_gtk_eval(WompratGTK *h, const char *s) {
  webkit_web_view_evaluate_javascript(h->shell, s, -1, NULL, NULL, NULL, NULL,
                                      NULL);
}
static void queried(GObject *view, GAsyncResult *result, gpointer p) {
  GError *error = NULL;
  JSCValue *value = webkit_web_view_evaluate_javascript_finish(
      WEBKIT_WEB_VIEW(view), result, &error);
  char *text = value ? jsc_value_to_string(value) : g_strdup("");
  goWompratQueryResult((uintptr_t)p, text);
  g_free(text);
  if (value)
    g_object_unref(value);
  if (error)
    g_error_free(error);
}
void womprat_gtk_query(WompratGTK *h, const char *id, const char *s,
                       uintptr_t result) {
  BrowserTab *t = g_hash_table_lookup(h->tabs, id);
  webkit_web_view_evaluate_javascript(t ? t->view : h->shell, s, -1, NULL, NULL,
                                      NULL, queried, (gpointer)result);
}
void womprat_gtk_quit(WompratGTK *h) { gtk_widget_destroy(h->window); }
static void data_cleared(GObject *data, GAsyncResult *result, gpointer p) {
  GError *error = NULL;
  gboolean ok = webkit_website_data_manager_clear_finish(
      WEBKIT_WEBSITE_DATA_MANAGER(data), result, &error);
  goWompratQueryResult((uintptr_t)p, ok ? "" : "Browser data deletion failed");
  if (error)
    g_error_free(error);
}
typedef struct {
  uintptr_t result;
  char *domain;
  GList *sites;
} DeleteCookies;
static void domains_deleted(GObject *data, GAsyncResult *result, gpointer p) {
  DeleteCookies *request = p;
  GError *error = NULL;
  gboolean ok = webkit_website_data_manager_remove_finish(
      WEBKIT_WEBSITE_DATA_MANAGER(data), result, &error);
  goWompratQueryResult(request->result, ok ? "" : "Cookie deletion failed");
  if (error)
    g_error_free(error);
  g_list_free_full(request->sites, (GDestroyNotify)webkit_website_data_unref);
  g_free(request->domain);
  g_free(request);
}
static void domains_fetched(GObject *data, GAsyncResult *result, gpointer p) {
  DeleteCookies *request = p;
  GError *error = NULL;
  GList *sites = webkit_website_data_manager_fetch_finish(
      WEBKIT_WEBSITE_DATA_MANAGER(data), result, &error);
  GList *selected = NULL;
  char *suffix = g_strdup_printf(".%s", request->domain);
  for (GList *i = sites; i; i = i->next) {
    const char *name = webkit_website_data_get_name(i->data);
    if (!g_ascii_strcasecmp(name, request->domain) ||
        g_str_has_suffix(name, suffix))
      selected = g_list_prepend(selected, i->data);
  }
  g_free(suffix);
  request->sites = sites;
  if (error || !selected) {
    goWompratQueryResult(request->result,
                         error ? "Cookie enumeration failed" : "");
    if (error)
      g_error_free(error);
    g_list_free_full(sites, (GDestroyNotify)webkit_website_data_unref);
    g_free(request->domain);
    g_free(request);
  } else {
    webkit_website_data_manager_remove(WEBKIT_WEBSITE_DATA_MANAGER(data),
                                       WEBKIT_WEBSITE_DATA_COOKIES, selected,
                                       NULL, domains_deleted, request);
    g_list_free(selected);
  }
}
static void cookies_listed(GObject *manager,GAsyncResult *result,gpointer p){
  GError *error=NULL;GList *cookies=webkit_cookie_manager_get_all_cookies_finish(WEBKIT_COOKIE_MANAGER(manager),result,&error);
  GString *domains=g_string_new("");if(!error)for(GList *i=cookies;i;i=i->next){const char *domain=soup_cookie_get_domain(i->data);if(domain&&!strchr(domain,'\n')){g_string_append(domains,domain);g_string_append_c(domains,'\n');}}
  goWompratQueryResult((uintptr_t)p,error?"!cookie enumeration failed":domains->str);g_string_free(domains,TRUE);g_list_free_full(cookies,(GDestroyNotify)soup_cookie_free);if(error)g_error_free(error);
}
void womprat_gtk_data(WompratGTK *h, int op, const char *domain,
                      uintptr_t result) {
  WebKitWebsiteDataManager *data =
      webkit_web_context_get_website_data_manager(h->content);
  if(op==3){webkit_cookie_manager_get_all_cookies(webkit_web_context_get_cookie_manager(h->content),NULL,cookies_listed,(gpointer)result);return;}
  if (op == 1 && *domain) {
    DeleteCookies *request = g_new0(DeleteCookies, 1);
    request->result = result;
    request->domain = g_strdup(domain);
    webkit_website_data_manager_fetch(data, WEBKIT_WEBSITE_DATA_COOKIES, NULL,
                                      domains_fetched, request);
    return;
  }
  WebKitWebsiteDataTypes types =
      op == 0
          ? (WEBKIT_WEBSITE_DATA_MEMORY_CACHE | WEBKIT_WEBSITE_DATA_DISK_CACHE)
      : op == 1 ? WEBKIT_WEBSITE_DATA_COOKIES
                : WEBKIT_WEBSITE_DATA_ALL;
  webkit_website_data_manager_clear(data, types, 0, NULL, data_cleared,
                                    (gpointer)result);
}
static void shell_message(WebKitUserContentManager *m,
                          WebKitJavascriptResult *r, gpointer p) {
  WompratGTK *h = p;
  char *s = jsc_value_to_string(webkit_javascript_result_get_js_value(r));
  if (s) {
    goWompratMessage(h->handle, "", s);
    g_free(s);
  }
}
static gboolean shell_policy(WebKitWebView *v, WebKitPolicyDecision *decision,
                             WebKitPolicyDecisionType kind, gpointer p) {
  if (kind == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION ||
      kind == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
    WebKitNavigationAction *action =
        webkit_navigation_policy_decision_get_navigation_action(
            WEBKIT_NAVIGATION_POLICY_DECISION(decision));
    const char *uri = webkit_uri_request_get_uri(
        webkit_navigation_action_get_request(action));
    WompratGTK *h = p;
    const char *current = h->shell_uri;
    GUri *a = current ? g_uri_parse(current, 0, NULL) : NULL,
         *b = uri ? g_uri_parse(uri, 0, NULL) : NULL;
    gboolean same = a && b &&
                    g_strcmp0(g_uri_get_scheme(a), g_uri_get_scheme(b)) == 0 &&
                    g_strcmp0(g_uri_get_host(a), g_uri_get_host(b)) == 0 &&
                    g_uri_get_port(a) == g_uri_get_port(b);
    if (a)
      g_uri_unref(a);
    if (b)
      g_uri_unref(b);
    if (current && !same) {
      webkit_policy_decision_ignore(decision);
      return TRUE;
    }
  }
  return FALSE;
}
static void browser_zoom(BrowserTab *t, int direction) {
  double zoom = direction == 0 ? 1.0 :
      MAX(0.3, MIN(3.0, webkit_web_view_get_zoom_level(t->view) + direction * 0.1));
  webkit_web_view_set_zoom_level(t->view, zoom);
}
static gboolean browser_key(GtkWidget *widget,GdkEventKey *event,gpointer p){
  BrowserTab *t=p;
  if((event->state&GDK_CONTROL_MASK) && !(event->state&GDK_MOD1_MASK)){
    if(event->keyval==GDK_KEY_plus||event->keyval==GDK_KEY_equal||event->keyval==GDK_KEY_KP_Add){browser_zoom(t,1);return TRUE;}
    if(event->keyval==GDK_KEY_minus||event->keyval==GDK_KEY_underscore||event->keyval==GDK_KEY_KP_Subtract){browser_zoom(t,-1);return TRUE;}
    if(event->keyval==GDK_KEY_0||event->keyval==GDK_KEY_parenright||event->keyval==GDK_KEY_KP_0){browser_zoom(t,0);return TRUE;}
  }
  return FALSE;
}
static gboolean deny_permission(WebKitWebView *v,
                                WebKitPermissionRequest *request, gpointer p) {
  webkit_permission_request_deny(request);
  return TRUE;
}
static void content_message(WebKitUserContentManager *m,
                            WebKitJavascriptResult *r, gpointer p) {
  BrowserTab *t = p;
  char *s = jsc_value_to_string(webkit_javascript_result_get_js_value(r));
  if (s) {
    goWompratMessage(t->host->handle, t->id, s);
    g_free(s);
  }
}
static void layout(WompratGTK *h) {
  GHashTableIter i;
  gpointer key, value;
  g_hash_table_iter_init(&i, h->tabs);
  while (g_hash_table_iter_next(&i, &key, &value)) {
    BrowserTab *t = value;
    gtk_widget_set_margin_top(GTK_WIDGET(t->view),h->chrome);
  }
}
static void allocated(GtkWidget *w, GtkAllocation *a, gpointer p) { layout(p); }
static void report(BrowserTab *t, int event, const char *detail, int ok) {
  const char *uri = webkit_web_view_get_uri(t->view);
  const char *title = webkit_web_view_get_title(t->view);
  goWompratEvent(t->host->handle, t->id, event, (char *)(uri ? uri : ""),
                 (char *)(detail ? detail : (title ? title : "")), ok,
                 (webkit_web_view_can_go_back(t->view) ? 1 : 0) |
                     (webkit_web_view_can_go_forward(t->view) ? 2 : 0));
}
static void load(WebKitWebView *v, WebKitLoadEvent event, gpointer p) {
  BrowserTab *t = p;
  if (event == WEBKIT_LOAD_STARTED) {
    t->failed = FALSE;
    report(t, 1, NULL, 1);
  }
  if (event == WEBKIT_LOAD_REDIRECTED)
    report(t, 1, NULL, 1);
  if (event == WEBKIT_LOAD_COMMITTED)
    report(t, 2, NULL, 1);
  if (event == WEBKIT_LOAD_FINISHED && !t->failed)
    report(t, 3, NULL, 1);
}
static gboolean failed(WebKitWebView *v, WebKitLoadEvent event, const char *uri,
                       GError *error, gpointer p) {
  BrowserTab *t = p;
  t->failed = TRUE;
  if (error->domain == WEBKIT_NETWORK_ERROR &&
      error->code == WEBKIT_NETWORK_ERROR_CANCELLED)
    return FALSE;
  report(t, 3, error->message, 0);
  return TRUE;
}
static void title_changed(GObject *v, GParamSpec *param, gpointer p) {
  report(p, 2, NULL, 1);
}
static cairo_status_t favicon_bytes(void *p, const unsigned char *data,
                                    unsigned int length) {
  GByteArray *bytes = p;
  // 1500 PNG bytes fit within the shared 2048-byte inline favicon limit.
  if (length > 1500 || bytes->len > 1500 - length)
    return CAIRO_STATUS_WRITE_ERROR;
  g_byte_array_append(bytes, data, length);
  return CAIRO_STATUS_SUCCESS;
}
static void favicon_changed(GObject *view, GParamSpec *param, gpointer p) {
  cairo_surface_t *source = webkit_web_view_get_favicon(WEBKIT_WEB_VIEW(view));
  if (!source || cairo_surface_get_type(source) != CAIRO_SURFACE_TYPE_IMAGE)
    return;
  int width = cairo_image_surface_get_width(source),
      height = cairo_image_surface_get_height(source);
  if (width <= 0 || height <= 0)
    return;
  cairo_surface_t *icon = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, 16, 16);
  cairo_t *cr = cairo_create(icon);
  cairo_scale(cr, 16.0 / width, 16.0 / height);
  cairo_set_source_surface(cr, source, 0, 0);
  cairo_paint(cr);
  cairo_destroy(cr);
  GByteArray *bytes = g_byte_array_new();
  if (cairo_surface_write_to_png_stream(icon, favicon_bytes, bytes) == CAIRO_STATUS_SUCCESS) {
    char *base64 = g_base64_encode(bytes->data, bytes->len);
    char *uri = g_strconcat("data:image/png;base64,", base64, NULL);
    report(p, 7, uri, 1);
    g_free(uri);
    g_free(base64);
  }
  g_byte_array_unref(bytes);
  cairo_surface_destroy(icon);
}
static gboolean policy(WebKitWebView *v, WebKitPolicyDecision *decision,
                       WebKitPolicyDecisionType kind, gpointer p) {
  BrowserTab *t = p;
  if (kind == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION ||
      kind == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
    WebKitNavigationAction *action =
        webkit_navigation_policy_decision_get_navigation_action(
            WEBKIT_NAVIGATION_POLICY_DECISION(decision));
    const char *uri = webkit_uri_request_get_uri(
        webkit_navigation_action_get_request(action));
    gboolean special =
        uri &&
        (g_str_has_prefix(uri, "ssh:") || g_str_has_prefix(uri, "vnc:") ||
         g_str_has_prefix(uri, "rdp:") || g_str_has_prefix(uri, "settings:"));
    if (kind == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION || special) {
      report(t, 4, uri, 1);
      webkit_policy_decision_ignore(decision);
      return TRUE;
    }
    if (!uri ||
        (!g_str_has_prefix(uri, "http://") &&
         !g_str_has_prefix(uri, "https://") && strcmp(uri, "about:blank"))) {
      webkit_policy_decision_ignore(decision);
      return TRUE;
    }
  }
  return FALSE;
}
static void terminated(WebKitWebView *v,
                       WebKitWebProcessTerminationReason reason, gpointer p) {
  report(p, 3, "Browser renderer exited. Reload this tab.", 0);
}
static void tab_free(gpointer p) {
  BrowserTab *t = p;
  g_signal_handlers_disconnect_by_data(t->view, t);
  g_signal_handlers_disconnect_by_data(
      webkit_web_view_get_user_content_manager(t->view), t);
  gtk_widget_destroy(GTK_WIDGET(t->view));
  g_object_unref(t->view);
  if (t->certificate)
    g_object_unref(t->certificate);
  g_free(t->certificate_uri);
  g_free(t->id);
  g_free(t);
}
static gboolean tls_failed(WebKitWebView *v, const char *uri,
                           GTlsCertificate *certificate,
                           GTlsCertificateFlags flags, gpointer p) {
  BrowserTab *t = p;
  t->failed = TRUE;
  if (t->certificate)
    g_object_unref(t->certificate);
  t->certificate = g_object_ref(certificate);
  g_free(t->certificate_uri);
  t->certificate_uri = g_strdup(uri);
  char *pem = NULL;
  g_object_get(certificate, "certificate-pem", &pem, NULL);
  goWompratEvent(t->host->handle, t->id, 5, (char *)uri, pem ? pem : "", flags,
                 0);
  g_free(pem);
  return TRUE;
}
static void cookies_ready(GObject *manager, GAsyncResult *result, gpointer p) {
  CookieRequest *request = p;
  GError *error = NULL;
  GList *cookies = webkit_cookie_manager_get_cookies_finish(
      WEBKIT_COOKIE_MANAGER(manager), result, &error);
  GString *value = g_string_new("");
  if (!error)
    for (GList *i = cookies; i; i = i->next) {
      SoupCookie *c = i->data;
      char *line = soup_cookie_to_set_cookie_header(c);
      if (line) {
        g_string_append(value, line);
        g_string_append_c(value, '\n');
        g_free(line);
      }
    }
  goWompratEvent(request->host, request->id, 6, request->url,
                 error ? "" : value->str, error ? 0 : 1, 0);
  g_list_free_full(cookies, (GDestroyNotify)soup_cookie_free);
  g_string_free(value, TRUE);
  if (error)
    g_error_free(error);
  g_free(request->id);
  g_free(request->url);
  g_free(request);
}
static void download_cookies(BrowserTab *t, const char *url) {
  const char *current = webkit_web_view_get_uri(t->view);
  GUri *a = current ? g_uri_parse(current, 0, NULL) : NULL,
       *b = g_uri_parse(url, 0, NULL);
  gboolean same = a && b &&
                  g_strcmp0(g_uri_get_scheme(a), g_uri_get_scheme(b)) == 0 &&
                  g_strcmp0(g_uri_get_host(a), g_uri_get_host(b)) == 0 &&
                  g_uri_get_port(a) == g_uri_get_port(b);
  if (a)
    g_uri_unref(a);
  if (b)
    g_uri_unref(b);
  if (!same) {
    goWompratEvent(t->host->handle, t->id, 6, (char *)url, "", 1, 0);
    return;
  }
  CookieRequest *r = g_new0(CookieRequest, 1);
  r->host = t->host->handle;
  r->id = g_strdup(t->id);
  r->url = g_strdup(url);
  webkit_cookie_manager_get_cookies(
      webkit_web_context_get_cookie_manager(t->host->content), url, NULL,
      cookies_ready, r);
}
static void download_started(WebKitWebContext *context,
                             WebKitDownload *download, gpointer p) {
  WompratGTK *h = p;
  WebKitWebView *view = webkit_download_get_web_view(download);
  GHashTableIter i;
  gpointer k, v;
  g_hash_table_iter_init(&i, h->tabs);
  while (g_hash_table_iter_next(&i, &k, &v)) {
    BrowserTab *t = v;
    if (t->view == view) {
      download_cookies(
          t, webkit_uri_request_get_uri(webkit_download_get_request(download)));
      break;
    }
  }
  // Native downloads must never write outside the managed exclusive-file path.
  webkit_download_cancel(download);
}
static void closed(GtkWidget *w, gpointer p) {
  WompratGTK *h = p;
  goWompratClosed(h->handle);
  gtk_main_quit();
}

WompratGTK *womprat_gtk_new(uintptr_t handle, const char *shell,
                            const char *profile, const char *proxy,
                            const char *bridge, const char *shell_bridge,
                            int debug) {
  if (!gtk_init_check(NULL, NULL))
    return NULL;
  WompratGTK *h = g_new0(WompratGTK, 1);
  h->handle = handle;
  h->chrome = 90;
  h->debug = debug;
  h->bridge = g_strdup(bridge);
  h->shell_uri = g_strdup(shell);
  h->tabs = g_hash_table_new_full(g_str_hash, g_str_equal, g_free, tab_free);
  WebKitWebsiteDataManager *data = webkit_website_data_manager_new(
      "base-data-directory", profile, "base-cache-directory", profile, NULL);
  WebKitNetworkProxySettings *settings =
      webkit_network_proxy_settings_new(proxy, NULL);
  webkit_website_data_manager_set_network_proxy_settings(
      data, WEBKIT_NETWORK_PROXY_MODE_CUSTOM, settings);
  webkit_network_proxy_settings_free(settings);
  h->content = webkit_web_context_new_with_website_data_manager(data);
  // Let WebKit fetch icons through the same fail-closed content proxy. Only
  // bounded inline pixels cross into the loopback shell, never a remote URL.
  char *favicons = g_build_filename(profile, "favicons", NULL);
  webkit_web_context_set_favicon_database_directory(h->content, favicons);
  g_free(favicons);
  char *cookie_path = g_build_filename(profile, "cookies.sqlite", NULL);
  webkit_cookie_manager_set_persistent_storage(webkit_web_context_get_cookie_manager(h->content),cookie_path,WEBKIT_COOKIE_PERSISTENT_STORAGE_SQLITE);
  g_free(cookie_path);
  g_object_unref(data);
  webkit_web_context_set_sandbox_enabled(h->content, TRUE);
  g_signal_connect(h->content, "download-started", G_CALLBACK(download_started),
                   h);
  // Shell context is isolated from content proxy, cookies, credentials and JS.
  WebKitWebContext *local = webkit_web_context_new_ephemeral();
  webkit_web_context_set_sandbox_enabled(local, TRUE);
  WebKitUserContentManager *manager = webkit_user_content_manager_new();
  webkit_user_content_manager_register_script_message_handler(manager,
                                                              "womprat");
  g_signal_connect(manager, "script-message-received::womprat",
                   G_CALLBACK(shell_message), h);
  WebKitUserScript *script = webkit_user_script_new(
      "window.__wompratNativeSend=function(s){window.webkit.messageHandlers."
      "womprat.postMessage(s)};",
      WEBKIT_USER_CONTENT_INJECT_TOP_FRAME,
      WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
  webkit_user_content_manager_add_script(manager, script);
  webkit_user_script_unref(script);
  script = webkit_user_script_new(
      shell_bridge, WEBKIT_USER_CONTENT_INJECT_TOP_FRAME,
      WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
  webkit_user_content_manager_add_script(manager, script);
  webkit_user_script_unref(script);
  h->shell =
      WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", local,
                                   "user-content-manager", manager, NULL));
  g_object_unref(manager);
  g_object_unref(local);
  webkit_settings_set_enable_developer_extras(
      webkit_web_view_get_settings(h->shell), debug);
  if(g_strcmp0(g_getenv("LIBGL_ALWAYS_SOFTWARE"),"1")==0)
    webkit_settings_set_hardware_acceleration_policy(webkit_web_view_get_settings(h->shell),WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER);
  h->window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_window_set_title(GTK_WINDOW(h->window), "Womprat");
  gtk_window_set_default_size(GTK_WINDOW(h->window), 1200, 800);
  h->layout = gtk_overlay_new();
  gtk_container_add(GTK_CONTAINER(h->window), h->layout);
  gtk_container_add(GTK_CONTAINER(h->layout),GTK_WIDGET(h->shell));
  g_signal_connect(h->shell, "decide-policy", G_CALLBACK(shell_policy), h);
  g_signal_connect(h->window, "destroy", G_CALLBACK(closed), h);
  g_signal_connect(h->layout, "size-allocate", G_CALLBACK(allocated), h);
  gtk_widget_show_all(h->window);
  webkit_web_view_load_uri(h->shell, shell);
  return h;
}
void womprat_gtk_run(WompratGTK *h) {
  gtk_main();
  g_hash_table_destroy(h->tabs);
  g_object_unref(h->content);
  g_free(h->active);
  g_free(h->bridge);
  g_free(h->shell_uri);
  g_free(h);
}
void womprat_gtk_chrome(WompratGTK *h, int px) {
  int scale = gtk_widget_get_scale_factor(h->window);
  h->chrome = MAX(24, MIN(512, px)) / MAX(1, scale);
  layout(h);
}
void womprat_gtk_focus(WompratGTK *h) {
  gtk_widget_grab_focus(GTK_WIDGET(h->shell));
}
void womprat_gtk_tab(WompratGTK *h, const char *id, const char *url, int op) {
  BrowserTab *t = g_hash_table_lookup(h->tabs, id);
  if (op == 0 && !t) {
    t = g_new0(BrowserTab, 1);
    t->host = h;
    t->id = g_strdup(id);
    WebKitUserContentManager *manager = webkit_user_content_manager_new();
    webkit_user_content_manager_register_script_message_handler(manager,
                                                                "womprat");
    g_signal_connect(manager, "script-message-received::womprat",
                     G_CALLBACK(content_message), t);
    WebKitUserScript *script = webkit_user_script_new(
        h->bridge, WEBKIT_USER_CONTENT_INJECT_TOP_FRAME,
        WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
    webkit_user_content_manager_add_script(manager, script);
    webkit_user_script_unref(script);
    t->view = WEBKIT_WEB_VIEW(g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context",
                                           h->content, "user-content-manager",
                                           manager, NULL));
    g_object_ref_sink(t->view);
    g_object_unref(manager);
    webkit_settings_set_enable_developer_extras(
        webkit_web_view_get_settings(t->view), h->debug);
    if(g_strcmp0(g_getenv("LIBGL_ALWAYS_SOFTWARE"),"1")==0)
      webkit_settings_set_hardware_acceleration_policy(webkit_web_view_get_settings(t->view),WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER);
    webkit_settings_set_enable_webrtc(webkit_web_view_get_settings(t->view),
                                      FALSE);
    g_signal_connect(t->view,"key-press-event",G_CALLBACK(browser_key),t);
    g_signal_connect(t->view, "permission-request", G_CALLBACK(deny_permission),
                     t);
    g_signal_connect(t->view, "load-failed-with-tls-errors",
                     G_CALLBACK(tls_failed), t);
    g_signal_connect(t->view, "load-changed", G_CALLBACK(load), t);
    g_signal_connect(t->view, "load-failed", G_CALLBACK(failed), t);
    g_signal_connect(t->view, "notify::title", G_CALLBACK(title_changed), t);
    g_signal_connect(t->view, "notify::favicon", G_CALLBACK(favicon_changed), t);
    g_signal_connect(t->view, "decide-policy", G_CALLBACK(policy), t);
    g_signal_connect(t->view, "web-process-terminated", G_CALLBACK(terminated),
                     t);
    gtk_widget_set_halign(GTK_WIDGET(t->view),GTK_ALIGN_FILL);
    gtk_widget_set_valign(GTK_WIDGET(t->view),GTK_ALIGN_FILL);
    gtk_widget_set_hexpand(GTK_WIDGET(t->view),TRUE);
    gtk_widget_set_vexpand(GTK_WIDGET(t->view),TRUE);
    gtk_overlay_add_overlay(GTK_OVERLAY(h->layout),GTK_WIDGET(t->view));
    g_hash_table_insert(h->tabs, g_strdup(id), t);
    layout(h);
  }
  if (op == 7) {
    g_free(h->active);
    h->active = NULL;
    GHashTableIter i;
    gpointer k, v;
    g_hash_table_iter_init(&i, h->tabs);
    while (g_hash_table_iter_next(&i, &k, &v))
      gtk_widget_hide(GTK_WIDGET(((BrowserTab *)v)->view));
    layout(h);
    return;
  }
  if (!t)
    return;
  switch (op) {
  case 1:
    webkit_web_view_load_uri(t->view, url);
    break;
  case 2:
    webkit_web_view_go_back(t->view);
    break;
  case 3:
    webkit_web_view_go_forward(t->view);
    break;
  case 4:
    webkit_web_view_reload(t->view);
    break;
  case 5: {
    womprat_gtk_tab(h, "", "", 7);
    h->active = g_strdup(id);
    gtk_widget_show(GTK_WIDGET(t->view));
    layout(h);
    gtk_widget_grab_focus(GTK_WIDGET(t->view));
    break;
  }
  case 6:
    gtk_widget_hide(GTK_WIDGET(t->view));
    break;
  case 8:
    g_hash_table_remove(h->tabs, id);
    break;
  case 9:
    if (t->certificate && t->certificate_uri &&
        !strcmp(url, t->certificate_uri)) {
      GUri *uri = g_uri_parse(url, 0, NULL);
      if (uri) {
        webkit_web_context_allow_tls_certificate_for_host(
            h->content, t->certificate, g_uri_get_host(uri));
        g_uri_unref(uri);
        webkit_web_view_load_uri(t->view, url);
      }
    }
    break;
  case 10:
    download_cookies(t, url);
    break;
  case 11: case 12: case 13:
    browser_zoom(t, op - 12);
    break;
  }
}
