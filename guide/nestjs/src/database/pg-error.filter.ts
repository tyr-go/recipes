import {
  ArgumentsHost,
  BadRequestException,
  Catch,
  ConflictException,
  GatewayTimeoutException,
  HttpException,
  ServiceUnavailableException,
} from '@nestjs/common';
import { BaseExceptionFilter } from '@nestjs/core';
import { DatabaseError } from 'pg';

//guide:pg-error-filter
// PgErrorFilter answers the errors of PostgreSQL that the services leave
// as they are with the statuses they mean for the client.
@Catch(DatabaseError)
export class PgErrorFilter extends BaseExceptionFilter {
  catch(error: DatabaseError, host: ArgumentsHost): void {
    super.catch(this.toHttp(error) ?? error, host);
  }

  private toHttp(error: DatabaseError): HttpException | undefined {
    switch (error.code) {
      case '23505': // unique_violation
        return new ConflictException('already exists');
      case '23503': // foreign_key_violation
        return new ConflictException('conflicts with related records');
      case '23514': // check_violation
        return new BadRequestException('a value is invalid');
      case '57014': // query_canceled, as by statement_timeout
        return new GatewayTimeoutException('the query took too long');
      case '40001': // serialization_failure
      case '40P01': // deadlock_detected
        return new ServiceUnavailableException('the query conflicted with another: try again');
    }
    return undefined; // a 500
  }
}
//guide:end
