import { SetMetadata } from '@nestjs/common';
import { Reflector } from '@nestjs/core';

export const IS_PUBLIC = 'isPublic';

// Public marks a controller or a route that anyone may call, without a token.
export const Public = () => SetMetadata(IS_PUBLIC, true);

// Roles marks a route that only callers with one of the roles may call.
export const Roles = Reflector.createDecorator<string[]>();
