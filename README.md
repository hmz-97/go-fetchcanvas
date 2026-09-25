# About
- This is a wip of a Go implementation of a Python tool I created to download the files from my courses in Canvas (CMU).
- **Benefits**:
  - Once you build your command properly, you can on a single command quickly download all of your files from all courses.
  - When you have them on disk, it is much easier to find the class slides, p-set pdf, csv dataset, or whatever file you need. Especially if you have your files indexed and a good search tool like Raycast or fzf. This way you don't need to be opening Canvas nearly as much to find stuff.
  - You're also sure that by the end of the semester you will have every single file course for your future archive when you lose access.
- I'm re-implementing this in Go to take advantage of Coroutines and fire multiple downloads at the same time sharing an http.Client struct. Obviously this makes it much faster than Python.

# Tbd:
- skip existing files
- add support for multiple cli args for course ids
- add support for course aliases
- add subfolder logic
- improve config.toml for similar functionality
- add config.toml setup command
- find a way to make this a claude skill or something
