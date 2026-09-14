#import <Cocoa/Cocoa.h>
void codexswitch_configure_window(void *ptr) {
 NSWindow *w=(NSWindow*)ptr;
 [w setFrameAutosaveName:@"CodexSwitch.MainWindow"];
 [w setFrameUsingName:@"CodexSwitch.MainWindow"];
 NSMenu *main=[NSApp mainMenu];
 NSMenuItem *app=[main itemAtIndex:0];
 NSMenu *menu=[app submenu];
 NSMenuItem *quit=[menu itemWithTitle:@"Quit CodexSwitch"];
 if(!quit){ quit=[[NSMenuItem alloc] initWithTitle:@"Quit CodexSwitch" action:@selector(terminate:) keyEquivalent:@"q"]; [quit setKeyEquivalentModifierMask:NSEventModifierFlagCommand]; [menu addItem:quit]; }
}
