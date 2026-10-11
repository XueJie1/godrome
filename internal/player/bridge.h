#include "mpv_client.h"
typedef struct gd_mpv gd_mpv;
gd_mpv *gd_open(void);
int gd_create(gd_mpv *p);
int gd_option(gd_mpv *p, const char *name, const char *value);
int gd_init(gd_mpv *p);
int gd_observe(gd_mpv *p, const char *name, int format);
int gd_command(gd_mpv *p, const char *a, const char *b, const char *c);
int gd_set(gd_mpv *p, const char *name, const char *value);
mpv_event *gd_wait(gd_mpv *p, double timeout);
void gd_close(gd_mpv *p);
