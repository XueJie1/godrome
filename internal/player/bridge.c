//go:build cgo

#include "bridge.h"
#include <locale.h>
#include <stdlib.h>
#ifdef _WIN32
#include <windows.h>
#define OPEN(n) LoadLibraryA(n)
#define SYMBOL(h,n) GetProcAddress(h,n)
#define CLOSE(h) FreeLibrary(h)
#else
#include <dlfcn.h>
#define OPEN(n) dlopen(n, RTLD_NOW | RTLD_LOCAL)
#define SYMBOL(h,n) dlsym(h,n)
#define CLOSE(h) dlclose(h)
#endif

struct gd_mpv {
 void *lib;
 mpv_handle *handle;
 mpv_handle *(*create)(void);
 int (*initialize)(mpv_handle *);
 int (*set_option_string)(mpv_handle *, const char *, const char *);
 int (*set_property_string)(mpv_handle *, const char *, const char *);
 int (*observe_property)(mpv_handle *, uint64_t, const char *, mpv_format);
 int (*command)(mpv_handle *, const char **);
 mpv_event *(*wait_event)(mpv_handle *, double);
 void (*terminate_destroy)(mpv_handle *);
};

gd_mpv *gd_open(void) {
 gd_mpv *p=calloc(1,sizeof(*p));
 if (!p) return NULL;
#ifdef _WIN32
 const char *names[]={"mpv-2.dll", "libmpv-2.dll", NULL};
#elif defined(__APPLE__)
 const char *names[]={"libmpv.2.dylib", "/opt/homebrew/lib/libmpv.2.dylib", "/usr/local/lib/libmpv.2.dylib", NULL};
#else
 const char *names[]={"libmpv.so.2", NULL};
#endif
 for(int i=0;names[i] && !p->lib;i++) p->lib=OPEN(names[i]);
 if(!p->lib) { free(p); return NULL; }
#define LOAD(field, name) p->field=(void *)SYMBOL(p->lib,name); if(!p->field) { gd_close(p); return NULL; }
 LOAD(create,"mpv_create")
 LOAD(initialize,"mpv_initialize")
 LOAD(set_option_string,"mpv_set_option_string")
 LOAD(set_property_string,"mpv_set_property_string")
 LOAD(observe_property,"mpv_observe_property")
 LOAD(command,"mpv_command")
 LOAD(wait_event,"mpv_wait_event")
 LOAD(terminate_destroy,"mpv_terminate_destroy")
 return p;
}
int gd_create(gd_mpv *p) {
 /* libmpv rejects mpv_create unless LC_NUMERIC is C. A desktop locale such as zh_CN sets it otherwise. */
 setlocale(LC_NUMERIC, "C");
 p->handle=p->create();
 return p->handle?0:-1;
}
int gd_option(gd_mpv *p,const char *n,const char *v) { return p->set_option_string(p->handle,n,v); }
int gd_init(gd_mpv *p) { return p->initialize(p->handle); }
int gd_observe(gd_mpv *p,const char *n,int f) { return p->observe_property(p->handle,0,n,f); }
int gd_command(gd_mpv *p,const char *a,const char *b,const char *c) {
 const char *args[]={a,b,c,NULL}; return p->command(p->handle,args);
}
int gd_set(gd_mpv *p,const char *n,const char *v) { return p->set_property_string(p->handle,n,v); }
mpv_event *gd_wait(gd_mpv *p,double t) { return p->wait_event(p->handle,t); }
void gd_close(gd_mpv *p) {
 if(!p) return;
 if(p->handle) p->terminate_destroy(p->handle);
 if(p->lib) CLOSE(p->lib);
 free(p);
}
