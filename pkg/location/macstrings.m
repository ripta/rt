#import "macstrings.h"

unsigned long nsarray_length(NSArray *arr) {
  if (arr == NULL) {
    return 0;
  }
  return arr.count;
}

const void *nsarray_object_at_index(NSArray *arr, unsigned long idx) {
  if (arr == NULL) {
    return NULL;
  }
  return [arr objectAtIndex:idx];
}

const char *nsstring_to_charstar(NSString *str) {
  if (str == NULL) {
    return NULL;
  }

  const char *cs = [str UTF8String];
  return cs;
}