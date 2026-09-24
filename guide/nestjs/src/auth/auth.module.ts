import { Module } from '@nestjs/common';
import { APP_GUARD } from '@nestjs/core';
import { JwtModule } from '@nestjs/jwt';
import { AuthGuard } from './auth.guard';
import { RolesGuard } from './roles.guard';

// AuthModule guards every route: first who calls, then what they may.
@Module({
  imports: [
    JwtModule.register({
      secret: process.env.JWT_SECRET,
      verifyOptions: { algorithms: ['HS256'] },
    }),
  ],
  providers: [
    { provide: APP_GUARD, useClass: AuthGuard },
    { provide: APP_GUARD, useClass: RolesGuard },
  ],
})
export class AuthModule {}
