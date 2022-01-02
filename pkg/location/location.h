#import <CoreLocation/CoreLocation.h>

typedef struct _Placemark {
  NSString *name;
  NSString *isoCountryCode;
  NSString *country;
  NSString *postalCode;
  NSString *administrativeArea;
  NSString *subadministrativeArea;
  NSString *locality;
  NSString *sublocality;
  NSString *thoroughfare;
  NSString *subthoroughfare;
  NSString *region;
  NSString *inlandWater;
  NSString *ocean;
  NSArray<NSString *> *areasOfInterest;
} Placemark;

typedef struct _Location {
  double latitude;
  double longitude;
  double altitude;
  double ellipsoidalAltitude;
  double horizontalAccuracy;
  double verticalAccuracy;
  int timestamp;
  BOOL hasPlacemark;
} Location;

int currentLocation(Location *loc, Placemark *pla);