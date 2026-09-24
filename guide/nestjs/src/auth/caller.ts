import { createParamDecorator, ExecutionContext } from '@nestjs/common';

// Caller is an authenticated caller: the subject of its token and its roles.
export interface Caller {
  id: string;
  roles: string[];
}

// CurrentCaller is the caller of a request, which AuthGuard found.
export const CurrentCaller = createParamDecorator(
  (_: unknown, context: ExecutionContext): Caller => context.switchToHttp().getRequest().user,
);
