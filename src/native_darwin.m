#import <Cocoa/Cocoa.h>

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
