import 'reflect-metadata';
import { ValidationPipe } from '@nestjs/common';
import { HttpAdapterHost, NestFactory } from '@nestjs/core';
import { DocumentBuilder, SwaggerModule } from '@nestjs/swagger';
import { AppModule } from './app.module';
import { PgErrorFilter } from './database/pg-error.filter';

//guide:bootstrap
async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  app.useGlobalPipes(new ValidationPipe({ whitelist: true, transform: true }));
  const { httpAdapter } = app.get(HttpAdapterHost);
  app.useGlobalFilters(new PgErrorFilter(httpAdapter));
  app.enableCors({ origin: process.env.ORIGINS?.split(','), exposedHeaders: ['Location'] });
  app.enableShutdownHooks();

  const config = new DocumentBuilder().setTitle('tasks').setVersion('1.0.0').addBearerAuth().build();
  SwaggerModule.setup('docs', app, () => SwaggerModule.createDocument(app, config));

  await app.listen(process.env.PORT ?? 8080);
}
//guide:end

void bootstrap();
