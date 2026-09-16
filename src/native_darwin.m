#import <Cocoa/Cocoa.h>
#include <libproc.h>

// NSRunningApplication.launchDate 可能為 nil，改用核心程序資訊辨識 PID 世代。
static double codexswitch_process_start(pid_t pid) {
 struct proc_bsdinfo info = {0};
 if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) return 0;
 return (double)info.pbi_start_tvsec + (double)info.pbi_start_tvusec / 1000000.0;
}


int codexswitch_button_hints(void) {
 id value = [[NSUserDefaults standardUserDefaults] objectForKey:@"CodexSwitch.ButtonHints"];
 return value == nil ? 1 : [value boolValue];
}
void codexswitch_save_button_hints(int enabled) {
 [[NSUserDefaults standardUserDefaults] setBool:enabled != 0 forKey:@"CodexSwitch.ButtonHints"];
}

// 透過 macOS responder chain，讓焦點所在的 WebView 或輸入欄位處理編輯操作。
static NSMenuItem *addItem(NSMenu *menu, NSString *title, SEL action,
                           NSString *key, NSEventModifierFlags modifiers) {
 NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key];
 item.keyEquivalentModifierMask = modifiers;
 [menu addItem:item];
 return item;
}

static NSMenu *addMenu(NSMenu *main, NSString *title) {
 NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:@""];
 NSMenu *menu = [[NSMenu alloc] initWithTitle:title];
 item.submenu = menu;
 [main addItem:item];
 return menu;
}

void codexswitch_configure_window(void *ptr) {
 NSWindow *w = (NSWindow *)ptr;
 [w setFrameAutosaveName:@"CodexSwitch.MainWindow"];
 [w setFrameUsingName:@"CodexSwitch.MainWindow"];
 w.collectionBehavior |= NSWindowCollectionBehaviorFullScreenPrimary;

 NSEventModifierFlags cmd = NSEventModifierFlagCommand;
 NSMenu *main = [[NSMenu alloc] initWithTitle:@""];
 NSMenu *app = addMenu(main, @"CodexSwitch");
 addItem(app, @"關於 CodexSwitch", @selector(orderFrontStandardAboutPanel:), @"", 0);
 [app addItem:[NSMenuItem separatorItem]];
 NSMenuItem *services = addItem(app, @"服務", nil, @"", 0);
 services.submenu = [[NSMenu alloc] initWithTitle:@"服務"];
 [NSApp setServicesMenu:services.submenu];
 [app addItem:[NSMenuItem separatorItem]];
 addItem(app, @"隱藏 CodexSwitch", @selector(hide:), @"h", cmd);
 addItem(app, @"隱藏其他程式", @selector(hideOtherApplications:), @"h", cmd | NSEventModifierFlagOption);
 addItem(app, @"顯示全部", @selector(unhideAllApplications:), @"", 0);
 [app addItem:[NSMenuItem separatorItem]];
 addItem(app, @"結束 CodexSwitch", @selector(terminate:), @"q", cmd);

 NSMenu *file = addMenu(main, @"檔案");
 addItem(file, @"關閉視窗", @selector(performClose:), @"w", cmd);
 NSMenu *edit = addMenu(main, @"編輯");
 addItem(edit, @"復原", @selector(undo:), @"z", cmd);
 addItem(edit, @"重做", @selector(redo:), @"z", cmd | NSEventModifierFlagShift);
 [edit addItem:[NSMenuItem separatorItem]];
 addItem(edit, @"剪下", @selector(cut:), @"x", cmd);
 addItem(edit, @"複製", @selector(copy:), @"c", cmd);
 addItem(edit, @"貼上", @selector(paste:), @"v", cmd);
 addItem(edit, @"貼上並符合樣式", @selector(pasteAsPlainText:), @"v", cmd | NSEventModifierFlagOption | NSEventModifierFlagShift);
 addItem(edit, @"全選", @selector(selectAll:), @"a", cmd);

 NSMenu *view = addMenu(main, @"顯示");
 addItem(view, @"切換全螢幕", @selector(toggleFullScreen:), @"f", cmd | NSEventModifierFlagControl);
 NSMenu *window = addMenu(main, @"視窗");
 addItem(window, @"縮小", @selector(performMiniaturize:), @"m", cmd);
 addItem(window, @"縮放", @selector(performZoom:), @"", 0);
 [window addItem:[NSMenuItem separatorItem]];
 addItem(window, @"全部移到最前面", @selector(arrangeInFront:), @"", 0);
 [NSApp setWindowsMenu:window];
 [NSApp setMainMenu:main];
}

// 只列出主 App，不包含 Helper、CLI 或其他同名程式。
char *codexswitch_instances(void) {
 @autoreleasepool {
  NSMutableArray *items = [NSMutableArray array];
  for (NSRunningApplication *app in [NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.openai.codex"]) {
   if (!app.terminated) {
    [items addObject:@{@"pid": @(app.processIdentifier), @"started": @(codexswitch_process_start(app.processIdentifier))}];
   }
  }
  NSData *data = [NSJSONSerialization dataWithJSONObject:items options:0 error:nil];
  return data ? strdup([[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String) : NULL;
 }
}

#include <sys/sysctl.h>
#include <stdlib.h>
#include <string.h>
char *codexswitch_procargs(int pid, int *length) {
 int limit = 0; size_t size = sizeof(limit); int argmax[] = {CTL_KERN, KERN_ARGMAX};
 if (sysctl(argmax, 2, &limit, &size, NULL, 0) != 0 || limit <= 0) return NULL;
 char *buffer = malloc(limit); if (!buffer) return NULL;
 int mib[] = {CTL_KERN, KERN_PROCARGS2, pid}; size = limit;
 if (sysctl(mib, 3, buffer, &size, NULL, 0) != 0) { free(buffer); return NULL; }
 *length = (int)size; return buffer;
}
int codexswitch_terminate_instance(int pid, double started) {
 @autoreleasepool {
  NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
  if (!app || app.terminated) return 1;
  if (![app.bundleIdentifier isEqualToString:@"com.openai.codex"] || started <= 0 ||
      codexswitch_process_start(pid) != started) return 0;
  return [app terminate] ? 1 : 0;
 }
}
