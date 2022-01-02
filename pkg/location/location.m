#import <Foundation/Foundation.h>
#import <CoreLocation/CoreLocation.h>
#import "location.h"

@interface RTLocationDelegate: NSObject <CLLocationManagerDelegate> {
	CLLocationManager *mgr;
}

@property(readonly) NSInteger error;

- (CLLocation *) currentLocation;

@end


@implementation RTLocationDelegate

- (id) init {
	mgr = [[CLLocationManager alloc] init];
	mgr.delegate = self;
	mgr.desiredAccuracy = kCLLocationAccuracyBest;
	return self;
}

- (void) dealloc {
	[mgr release];
	[super dealloc];
}

- (CLLocation *) currentLocation {
	[mgr requestLocation];
	CFRunLoopRun();

	if (_error != 0) {
		return nil;
	}

	CLLocation *loc = mgr.location;
	if (loc.horizontalAccuracy < 0L) {
		return nil; // no location
	}

	return loc;
}

- (void) locationManager:(CLLocationManager *) mgr didUpdateLocations:(NSArray<CLLocation *> *) locations {
	CFRunLoopStop(CFRunLoopGetCurrent());
}

- (void) locationManager:(CLLocationManager *) mgr didFailWithError:(NSError *) error {
	_error = error.code;
	CFRunLoopStop(CFRunLoopGetCurrent());
}

@end


int currentLocation(Location *loc) {
	int enabled = [CLLocationManager locationServicesEnabled];
	if (!enabled) {
		return 64; // LocationErrorCodeSpecialDisabled in location.go
	}

	RTLocationDelegate *del = [[RTLocationDelegate alloc] init];
	CLLocation *raw = [del currentLocation];
	if (del.error != 0) {
		[del release];
		return del.error;
	}

	loc->latitude = raw.coordinate.latitude;
	loc->longitude = raw.coordinate.longitude;
	loc->altitude = raw.altitude;
	loc->ellipsoidalAltitude = raw.ellipsoidalAltitude;
	loc->horizontalAccuracy = raw.horizontalAccuracy;
	loc->verticalAccuracy = raw.verticalAccuracy;
	loc->timestamp = [raw.timestamp timeIntervalSince1970];

	[raw release];
	[del release];

	return 0;
}