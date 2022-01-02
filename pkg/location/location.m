#import "location.h"
#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>

@interface RTLocationDelegate : NSObject <CLLocationManagerDelegate> {
  CLLocationManager *mgr;
}

@property(readonly) NSInteger error;

- (CLLocation *)currentLocation;

@end

@implementation RTLocationDelegate

- (id)init {
  mgr = [[CLLocationManager alloc] init];
  mgr.delegate = self;
  mgr.desiredAccuracy = kCLLocationAccuracyBest;
  return self;
}

- (void)dealloc {
  [mgr release];
  [super dealloc];
}

- (CLLocation *)currentLocation {
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

- (void)locationManager:(CLLocationManager *)mgr
     didUpdateLocations:(NSArray<CLLocation *> *)locations {
  CFRunLoopStop(CFRunLoopGetCurrent());
}

- (void)locationManager:(CLLocationManager *)mgr
       didFailWithError:(NSError *)error {
  _error = error.code;
  CFRunLoopStop(CFRunLoopGetCurrent());
}

@end

int currentLocation(Location *loc, Placemark *pla) {
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

  __block CLPlacemark *place;
  CLGeocoder *geo = [[CLGeocoder alloc] init];
  dispatch_semaphore_t sem = dispatch_semaphore_create(0);
  [geo reverseGeocodeLocation:raw
            completionHandler:^(NSArray *places, NSError *error) {
              if (places && places.count > 0) {
                place = [places objectAtIndex:0];
              }
              dispatch_semaphore_signal(sem);
            }];
  if (![NSThread isMainThread]) {
    // wait max 5 seconds
    dispatch_semaphore_wait(sem,
                            dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
  } else {
    while (dispatch_semaphore_wait(sem, DISPATCH_TIME_NOW)) {
      [[NSRunLoop currentRunLoop]
             runMode:NSDefaultRunLoopMode
          beforeDate:[NSDate dateWithTimeIntervalSinceNow:0]];
    }
  }

  loc->latitude = raw.coordinate.latitude;
  loc->longitude = raw.coordinate.longitude;
  loc->altitude = raw.altitude;
  loc->ellipsoidalAltitude = raw.ellipsoidalAltitude;
  loc->horizontalAccuracy = raw.horizontalAccuracy;
  loc->verticalAccuracy = raw.verticalAccuracy;
  loc->timestamp = [raw.timestamp timeIntervalSince1970];

  loc->hasPlacemark = NO;
  if (place != NULL) {
    loc->hasPlacemark = YES;
    pla->name = [place name];
    pla->isoCountryCode = [place ISOcountryCode];
    pla->country = [place country];
    pla->postalCode = [place postalCode];
    pla->administrativeArea = [place administrativeArea];
    pla->subadministrativeArea = [place subAdministrativeArea];
    pla->locality = [place locality];
    pla->sublocality = [place subLocality];
    pla->thoroughfare = [place thoroughfare];
    pla->subthoroughfare = [place subThoroughfare];
    // 		if ([place region] != NULL) {
    // 			pla->region = [[place region] name];
    // 		}
    pla->inlandWater = [place inlandWater];
    pla->ocean = [place ocean];
    pla->areasOfInterest = [place areasOfInterest];
  }

  [geo release];
  [raw release];
  [del release];

  return 0;
}
