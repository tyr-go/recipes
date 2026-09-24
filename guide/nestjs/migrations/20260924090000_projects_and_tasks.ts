import type { Knex } from 'knex';

//guide:migration
export async function up(knex: Knex): Promise<void> {
  await knex.schema.createTable('projects', (t) => {
    t.uuid('id').primary(); // a UUIDv7, made by the service
    t.text('owner_id').notNullable();
    t.text('key').notNullable().checkRegex('^[A-Z][A-Z0-9]{1,9}$');
    t.text('name').notNullable().checkLength('<=', 100);
    t.integer('last_number').notNullable().defaultTo(0); // of the last task created
    t.timestamp('created_at', { useTz: true }).notNullable().defaultTo(knex.fn.now());
    t.unique(['owner_id', 'key']);
    t.index(['owner_id', 'id']);
  });

  // A project with tasks can't be deleted: the foreign key has no cascade.
  await knex.schema.createTable('tasks', (t) => {
    t.uuid('id').primary();
    t.uuid('project_id').notNullable().references('id').inTable('projects');
    t.integer('number').notNullable();
    t.text('title').notNullable().checkLength('<=', 200);
    t.text('status').notNullable().defaultTo('todo').checkIn(['todo', 'doing', 'done']);
    t.timestamp('due_at', { useTz: true });
    t.timestamp('created_at', { useTz: true }).notNullable().defaultTo(knex.fn.now());
    t.timestamp('updated_at', { useTz: true }).notNullable().defaultTo(knex.fn.now());
    t.unique(['project_id', 'number']);
    t.index(['project_id', 'id']);
  });
}

export async function down(knex: Knex): Promise<void> {
  await knex.schema.dropTable('tasks');
  await knex.schema.dropTable('projects');
}
//guide:end
