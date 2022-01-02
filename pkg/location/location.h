#import <CoreLocation/CoreLocation.h>

typedef struct _Location {
	double latitude;
	double longitude;
	double altitude;
	double ellipsoidalAltitude;
	double horizontalAccuracy;
	double verticalAccuracy;
	int timestamp;
} Location;

int currentLocation(Location *loc);