#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>

// Private XCTest event-synthesis interfaces. These ship inside the testing
// frameworks bundled with Xcode and are the only path to timestamped touch
// events with millisecond-precision offsets. The selectors below were verified
// present in Xcode 26's XCUIAutomation framework binary.

NS_ASSUME_NONNULL_BEGIN

// One pointer's path through a synthesized event. Offsets are seconds from the
// start of the event record.
@interface XCPointerEventPath : NSObject
- (instancetype)initForTouchAtPoint:(CGPoint)point offset:(double)offset;
- (void)moveToPoint:(CGPoint)point atOffset:(double)offset;
- (void)pressDownAtOffset:(double)offset;
- (void)liftUpAtOffset:(double)offset;
@end

// A complete synthesized event composed of one or more pointer paths.
// synthesizeWithError: delivers the event synchronously and returns whether it
// succeeded, which avoids the asynchronous completion-block path.
@interface XCSynthesizedEventRecord : NSObject
- (instancetype)initWithName:(NSString *)name
         interfaceOrientation:(NSInteger)interfaceOrientation;
- (void)addPointerEventPath:(XCPointerEventPath *)pointerEventPath;
- (BOOL)synthesizeWithError:(NSError *_Nullable *_Nullable)error;
@end

// The runner-side session that delivers synthesized events to the system under
// test.
@interface XCTRunnerDaemonSession : NSObject
+ (instancetype)sharedSession;
- (void)synthesizeEvent:(XCSynthesizedEventRecord *)eventRecord
             completion:(void (^)(NSError *_Nullable error))completion;
@end

// Runs the block and converts any raised NSException into an NSError so a
// testing-framework assertion does not terminate the runner process.
NS_INLINE BOOL CompanionRunCatching(void (^block)(void), NSError *_Nullable *_Nullable error) {
    @try {
        block();
        return YES;
    } @catch (NSException *exception) {
        if (error) {
            *error = [NSError errorWithDomain:@"dev.sanderling.companion"
                                         code:1
                                     userInfo:@{NSLocalizedDescriptionKey: exception.reason ?: exception.name}];
        }
        return NO;
    }
}

NS_ASSUME_NONNULL_END
