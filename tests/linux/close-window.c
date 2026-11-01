// Send the WM_DELETE_WINDOW protocol GTK handles, without a window manager.
// xdotool windowclose destroys the X window externally and skips GTK shutdown.
#include <X11/Xlib.h>
#include <stdlib.h>
int main(int argc, char **argv) {
  if (argc != 2) return 2;
  Display *display = XOpenDisplay(NULL);
  if (!display) return 1;
  Window window = strtoul(argv[1], NULL, 0);
  XEvent event = {0};
  event.xclient.type = ClientMessage;
  event.xclient.window = window;
  event.xclient.message_type = XInternAtom(display, "WM_PROTOCOLS", False);
  event.xclient.format = 32;
  event.xclient.data.l[0] = XInternAtom(display, "WM_DELETE_WINDOW", False);
  event.xclient.data.l[1] = CurrentTime;
  int ok = XSendEvent(display, window, False, NoEventMask, &event);
  XSync(display, False);
  XCloseDisplay(display);
  return ok ? 0 : 1;
}
