import { CanActivate, ExecutionContext, ForbiddenException, Injectable } from '@nestjs/common';
import { Reflector } from '@nestjs/core';
import type { Caller } from './caller';
import { Roles } from './decorators';

//guide:roles-guard
// RolesGuard lets through the requests of the routes that Roles marks only
// if their caller has one of the roles.
@Injectable()
export class RolesGuard implements CanActivate {
  constructor(private readonly reflector: Reflector) {}

  canActivate(context: ExecutionContext): boolean {
    const roles = this.reflector.get(Roles, context.getHandler());
    if (!roles) {
      return true;
    }
    const { user } = context.switchToHttp().getRequest<{ user: Caller }>();
    if (!roles.some((role) => user.roles.includes(role))) {
      throw new ForbiddenException(`requires the role ${roles.join(' or ')}`);
    }
    return true;
  }
}
//guide:end
