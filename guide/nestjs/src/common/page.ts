import { BadRequestException } from '@nestjs/common';

// Page is a page of a list, newest first. The next page starts after the
// last item of this one: send its next_cursor as the cursor of the request.
export interface Page<T> {
  items: T[];
  next_cursor?: string;
}

// A cursor is the id of the last item of a page, in base64.

export function encodeCursor(id: string): string {
  return Buffer.from(id.replaceAll('-', ''), 'hex').toString('base64url');
}

// decodeCursor returns the id of cursor, or undefined for the first page.
export function decodeCursor(cursor?: string): string | undefined {
  if (!cursor) {
    return undefined;
  }
  const hex = Buffer.from(cursor, 'base64url').toString('hex');
  if (hex.length !== 32) {
    throw new BadRequestException(['cursor must be the next_cursor of a page']);
  }
  return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join('-');
}
