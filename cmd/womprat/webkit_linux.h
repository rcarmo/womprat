#ifndef WOMPRAT_WEBKIT_H
#define WOMPRAT_WEBKIT_H
#include <stdint.h>
typedef struct WompratGTK WompratGTK;
WompratGTK *womprat_gtk_new(uintptr_t handle, const char *shell,
                            const char *profile, const char *proxy,
                            const char *bridge, const char *shell_bridge,
                            int debug);
void womprat_gtk_run(WompratGTK *host);
void womprat_gtk_dispatch(uintptr_t handle);
void womprat_gtk_eval(WompratGTK *host, const char *script);
void womprat_gtk_query(WompratGTK *host, const char *id, const char *script,
                       uintptr_t result);
void womprat_gtk_quit(WompratGTK *host);
void womprat_gtk_data(WompratGTK *host, int operation, const char *domain,
                      uintptr_t result);
void womprat_gtk_tab(WompratGTK *host, const char *id, const char *url,
                     int operation);
void womprat_gtk_chrome(WompratGTK *host, int px);
void womprat_gtk_focus(WompratGTK *host);
#endif
