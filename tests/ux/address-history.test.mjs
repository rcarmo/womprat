import { test, expect } from 'bun:test';
import { addressHistorySuggestions } from '../../cmd/womprat/frontend/address-history.js';

test('matching ranks hostname prefixes ahead of substring matches and deduplicates results',()=>{
  const values=['https://other.example/flint','https://flint.local:8443/','flint search','https://FLINT.example/','https://flint.local:8443/'];
  expect(addressHistorySuggestions('flint',values)).toEqual(['https://flint.local:8443/','flint search','https://FLINT.example/','https://other.example/flint']);
  expect(addressHistorySuggestions('missing',values)).toEqual([]);
});
test('empty input lists at most four recent values and preserves Unicode search phrases',()=>{
  expect(addressHistorySuggestions('', ['café 日本語','query 2','query 3','query 4','query 5'])).toEqual(['café 日本語','query 2','query 3','query 4']);
  expect(addressHistorySuggestions('日本', ['café 日本語'])).toEqual(['café 日本語']);
});
test('malformed stored entries cannot create unsafe completion targets',()=>{
  expect(addressHistorySuggestions('', ['valid',null,{},'javascript:alert(1)','file:///secret','hello\nworld'])).toEqual(['valid']);
});
