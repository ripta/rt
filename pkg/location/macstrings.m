#import "macstrings.h"

unsigned long nsarray_length(void* arr) {
  if (arr == NULL) {
    return 0;
  }
  NSArray *na = *((__unsafe_unretained NSArray **)(arr));
  return na.count;
}

const void *nsarray_object_at_index(void* arr, unsigned long idx) {
  if (arr == NULL) {
    return NULL;
  }
  NSArray *na = *((__unsafe_unretained NSArray **)(arr));
  return [na objectAtIndex:idx];
}

const char *nsstring_to_charstar(void* str) {
  if (str == NULL) {
    return NULL;
  }

  NSString *s = *((__unsafe_unretained NSString **)(str));
  const char *cs = [s UTF8String];
  return cs;
}
