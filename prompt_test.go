package promptweaver

import "testing"

func Test_UserPayload_JSXProps_SplitsIntoFiles(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SectionPlugin{Name: "think"})
	reg.Register(SectionPlugin{Name: "write-file", Aliases: []string{"create-file"}})
	reg.Register(SectionPlugin{Name: "summary"})

	var files []SectionEvent
	sink := NewHandlerSink()
	sink.RegisterHandler("write-file", func(ev SectionEvent) { files = append(files, ev) })

	eng := NewEngine(reg)
	reader := &chunkedReader{data: []byte(src), chunk: 96}
	if err := eng.ProcessStream(reader, sink); err != nil {
		t.Fatal(err)
	}

	if len(files) != 4 {
		t.Fatalf("want 4 files, got %d", len(files))
	}
	want := []string{
		"app/todo/page.tsx",
		"app/todo/components/TodoItem.tsx",
		"app/todo/components/TodoForm.tsx",
		"app/todo/api/todos.ts",
	}
	for i, p := range want {
		if files[i].Attrs["path"] != p {
			t.Fatalf("file[%d] path want %q got %q", i, p, files[i].Attrs["path"])
		}
	}
}

const src = `<think>
• Create a Todo App with time reminder feature
• Use Next.js 14+ with App Router and Server Components
• Files: app/todo/page.tsx, app/todo/components/TodoItem.tsx, app/todo/components/TodoForm.tsx, app/todo/api/todos.ts
• Test: renders todo list, adds new todo, and sets reminder
• Risk: handling time zones and reminders across different devices
</think>
<create-file path="app/todo/page.tsx" type="page">
import { TodoItem } from './components/TodoItem';
import { TodoForm } from './components/TodoForm';
import { getTodos } from './api/todos';

export default async function TodoPage() {
  const todos = await getTodos();

  return (
    <div className="max-w-md mx-auto p-4">
      <h1 className="text-3xl font-bold mb-4">Todo App</h1>
      <TodoForm />
      <ul>
        {todos.map((todo) => (
          <TodoItem key={todo.id} todo={todo} />
        ))}
      </ul>
    </div>
  );
}
</create-file>
<create-file path="app/todo/components/TodoItem.tsx" type="component">
import { useState, useEffect } from 'react';

export function TodoItem({ todo }) {
  const [timeLeft, setTimeLeft] = useState(null);

  useEffect(() => {
    const intervalId = setInterval(() => {
      const now = new Date();
      const reminderTime = new Date(todo.reminder);
      const timeDiff = reminderTime - now;

      if (timeDiff < 0) {
        setTimeLeft('Reminder has passed');
      } else {
        const hours = Math.floor(timeDiff / (1000 * 60 * 60));
        const minutes = Math.floor((timeDiff % (1000 * 60 * 60)) / (1000 * 60));
        const seconds = Math.floor((timeDiff % (1000 * 60)) / 1000);

        setTimeLeft(${hours} hours ${minutes} minutes ${seconds} seconds);
      }
    }, 1000);

    return () => clearInterval(intervalId);
  }, [todo.reminder]);

  return (
    <li className="py-2 border-b border-gray-200">
      <span className="text-lg">{todo.title}</span>
      <span className="text-sm text-gray-500">{timeLeft}</span>
    </li>
  );
}
</create-file>
<create-file path="app/todo/components/TodoForm.tsx" type="component">
import { useState } from 'react';
import { createTodo } from '../api/todos';

export function TodoForm() {
  const [title, setTitle] = useState('');
  const [reminder, setReminder] = useState('');

  const handleSubmit = async (e) => {
    e.preventDefault();

    await createTodo({ title, reminder });
    setTitle('');
    setReminder('');
  };

  return (
    <form onSubmit={handleSubmit} className="mb-4">
      <input
        type="text"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        placeholder="Todo title"
        className="w-full p-2 border border-gray-200"
      />
      <input
        type="datetime-local"
        value={reminder}
        onChange={(e) => setReminder(e.target.value)}
        className="w-full p-2 border border-gray-200"
      />
      <button type="submit" className="bg-blue-500 text-white py-2 px-4">
        Add Todo
      </button>
    </form>
  );
}
</create-file>
<create-file path="app/todo/api/todos.ts" type="api">
import { NextApiRequest, NextApiResponse } from 'next';

const todos = [];

export async function getTodos() {
  return todos;
}

export async function createTodo(todo) {
  todos.push(todo);
}
</create-file>
<summary>Todo App with time reminder feature created; next step is to implement data persistence and handle time zones.</summary>`
