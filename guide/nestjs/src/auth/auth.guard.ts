import { CanActivate, ExecutionContext, Injectable, UnauthorizedException } from '@nestjs/common';
import { Reflector } from '@nestjs/core';
import { JwtService } from '@nestjs/jwt';
import type { Request } from 'express';
import type { Caller } from './caller';
import { IS_PUBLIC } from './decorators';

//guide:auth-guard
// AuthGuard lets through the requests with a valid bearer token, with
// their caller in request.user, and those of Public routes.
@Injectable()
export class AuthGuard implements CanActivate {
  constructor(
    private readonly jwt: JwtService,
    private readonly reflector: Reflector,
  ) {}

  async canActivate(context: ExecutionContext): Promise<boolean> {
    if (this.reflector.getAllAndOverride<boolean>(IS_PUBLIC, [context.getHandler(), context.getClass()])) {
      return true;
    }
    const request = context.switchToHttp().getRequest<Request & { user?: Caller }>();
    const [type, token] = request.headers.authorization?.split(' ') ?? [];
    if (type !== 'Bearer' || !token) {
      throw new UnauthorizedException('a bearer token is required');
    }
    try {
      const payload = await this.jwt.verifyAsync<{ sub: string; roles?: string[] }>(token);
      request.user = { id: payload.sub, roles: payload.roles ?? [] };
    } catch {
      throw new UnauthorizedException('the bearer token is invalid');
    }
    return true;
  }
}
//guide:end
